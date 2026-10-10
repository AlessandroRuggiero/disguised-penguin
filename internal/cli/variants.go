package cli

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"disguised-penguin/internal/container"
	"disguised-penguin/internal/models"

	"github.com/spf13/cobra"
)

// variantsFile declares this project's variants.
const variantsFile = ".dp/variants.json"

// variantBuildDir is where 'extend' scaffolds build files.
const variantBuildDir = ".dp/build"

// loadVariantsFile reads dir/.dp/variants.json. The map is never nil, so
// callers can assign into it.
func loadVariantsFile(dir string) (models.VariantConfigFile, error) {
	empty := models.VariantConfigFile{Variants: map[string]models.FileVariant{}}

	p := filepath.Join(dir, filepath.FromSlash(variantsFile))
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("failed to read %s: %w", p, err)
	}

	var config models.VariantConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return empty, fmt.Errorf("failed to parse %s: %w", p, err)
	}
	if config.Variants == nil {
		config.Variants = map[string]models.FileVariant{}
	}
	return config, nil
}

// loadVariants returns the project's variants keyed by the CLI they extend.
// A missing file yields an empty map; a malformed one is an error.
func loadVariants(dir string) (map[string]models.Variant, error) {
	config, err := loadVariantsFile(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to load variants file: %w", err)
	}

	variants := make(map[string]models.Variant, len(config.Variants))
	for cli, v := range config.Variants {
		if cli == "" {
			return nil, fmt.Errorf("invalid variant definition: empty CLI name")
		}
		if v.BuildFile == "" {
			return nil, fmt.Errorf("invalid variant definition for CLI %s: empty build_file", cli)
		}
		variants[cli] = models.Variant{
			Of:          cli,
			BuildFile:   v.BuildFile,
			LocalMounts: v.LocalMounts,
		}
	}
	return variants, nil
}

// variantTagPrefix keeps dp's tags out of the namespace upstream tags use.
const variantTagPrefix = "variant-"

// variantTagLen is how much of the project digest goes into the tag.
// The runtime's limit is 128 characters.
const variantTagLen = 32

// projectKey is what the variant tag hashes and the variants table stores.
func projectKey(projectDir string) string {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	return filepath.Clean(abs)
}

// variantImage is the CLI's image retagged with a digest of the project dir,
// so two projects never share a built image.
func variantImage(baseImage string, projectDir string) string {
	sum := sha256.Sum256([]byte(projectKey(projectDir)))
	return fmt.Sprintf("%s:%s%s", imageRepository(baseImage), variantTagPrefix, hex.EncodeToString(sum[:])[:variantTagLen])
}

// imageRepository strips any tag or digest, so the project tag replaces the
// base one instead of being appended to it.
func imageRepository(image string) string {
	name := image
	// Digest refs: tool@sha256:...
	if at := strings.Index(name, "@"); at != -1 {
		name = name[:at]
	}
	// A colon before the last "/" is a registry port, not a tag.
	if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name = name[:colon]
	}
	return strings.ToLower(name)
}

// isVariantBuilt reports whether this project's variant image exists locally.
func isVariantBuilt(runtime container.Runtime, baseImage string, projectDir string) (bool, error) {
	return imageExists(runtime, variantImage(baseImage, projectDir))
}

func imageExists(runtime container.Runtime, ref string) (bool, error) {
	id, err := imageID(runtime, ref)
	return id != "", err
}

// imageID returns the ref's image ID, or "" if it doesn't exist locally.
func imageID(runtime container.Runtime, ref string) (string, error) {
	// "images -q" prints an ID or nothing, both with exit 0, so a real
	// failure stays distinguishable from a missing image.
	cmd := exec.Command(string(runtime), "images", "-q", "--no-trunc", ref)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to check for image '%s' with %s: %w: %s", ref, runtime, err, strings.TrimSpace(stderr.String()))
	}
	return string(bytes.TrimSpace(out)), nil
}

const dpReadmeFile = ".dp/README.md"

//go:embed dpreadme.md
var dpReadme string

func writeFileIfMissing(p string, content string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", filepath.Dir(p), err)
	}

	// O_EXCL so an existing file can't be clobbered.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to create %s: %w", p, err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return false, fmt.Errorf("failed to write %s: %w", p, err)
	}
	return true, nil
}

// writeVariantDockerfile scaffolds a build file starting FROM the CLI's image.
// It returns false if the file already exists, which is left untouched.
func writeVariantDockerfile(buildFile string, baseImage string, baseOS string) (bool, error) {
	content := fmt.Sprintf("FROM %s\n\n", baseImage)
	if baseOS != "" {
		content += fmt.Sprintf("# Base OS: %s\n", baseOS)
	}
	content += "# Add what this project needs on top of the base image.\n# See .dp/README.md for what you can do here.\n"
	return writeFileIfMissing(buildFile, content)
}

func detectBaseOS(runtime container.Runtime, image string) (string, error) {
	cmd := exec.Command(string(runtime), "run", "--rm", "--pull=never", "--entrypoint", "cat", image, "/etc/os-release")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read /etc/os-release from '%s': %w: %s", image, err, strings.TrimSpace(stderr.String()))
	}
	return describeOSRelease(string(out)), nil
}

var packageManagers = map[string]string{
	"debian":   "apt-get",
	"ubuntu":   "apt-get",
	"fedora":   "dnf",
	"rhel":     "dnf",
	"alpine":   "apk",
	"suse":     "zypper",
	"opensuse": "zypper",
	"arch":     "pacman",
}

func describeOSRelease(content string) string {
	fields := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		fields[key] = strings.Trim(value, `"'`)
	}

	name := fields["PRETTY_NAME"]
	if name == "" {
		name = fields["NAME"]
	}
	if name == "" {
		return ""
	}

	for _, id := range append([]string{fields["ID"]}, strings.Fields(fields["ID_LIKE"])...) {
		if pm, ok := packageManagers[id]; ok {
			return fmt.Sprintf("%s (install packages with %s)", name, pm)
		}
	}
	return name
}

// completeVariantNames completes the CLIs this project declares a variant for.
func completeVariantNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	variants, err := loadVariants(cwd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for of := range variants {
		names = append(names, of)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

var localCmd = &cobra.Command{
	Use:     "local",
	Aliases: []string{"l"},
	Short:   "Commands that act on the current project only",
	Long:    "Commands that act on the current project only.\nThey read ./.dp and produce images that never leave this machine, unlike 'install' or 'update' which touch the shared, installed CLIs.",
}

var localVariantCmd = &cobra.Command{
	Use:     "variant",
	Aliases: []string{"v"},
	Short:   "Manage this project's variants",
	Long:    "Manage this project's variants.\nA variant is a project-local flavour of an installed CLI, declared in ./.dp/variants.json and built into an image tagged for this directory alone.",
}

var localVariantBuildCmd = &cobra.Command{
	Use:     "build [cli]",
	Aliases: []string{"b"},
	Short:   "Build this project's variant images",
	Long:    "Build the variant images declared in ./.dp/variants.json.\nWith no argument every variant in the file is built.",
	Example: `  dp local variant build
  dp local variant build claude`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeVariantNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		variants, err := loadVariants(cwd)
		if err != nil {
			return err
		}

		var targets []string
		if len(args) == 1 {
			if _, ok := variants[args[0]]; !ok {
				return fmt.Errorf("no variant of '%s' is declared in %s (declare one with 'dp local variant extend %s')", args[0], variantsFile, args[0])
			}
			targets = []string{args[0]}
		} else {
			for of := range variants {
				targets = append(targets, of)
			}
			// Stable order across runs.
			sort.Strings(targets)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no variants declared in %s (declare one with 'dp local variant extend [cli]')", variantsFile)
		}

		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			return err
		}

		var failed []string
		for i, of := range targets {
			if len(targets) > 1 {
				fmt.Printf("[%d/%d] Building variant of '%s'...\n", i+1, len(targets), of)
			}
			if err := buildVariant(runtime, variants[of], cwd); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to build variant of '%s': %v\n", of, err)
				failed = append(failed, of)
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed to build: %s", strings.Join(failed, ", "))
		}
		return nil
	},
}

// buildVariant builds one variant, with the project dir as build context.
func buildVariant(runtime container.Runtime, variant models.Variant, projectDir string) error {
	// Base image comes from the installed CLI, so it tracks updates.
	cli, err := store.GetCliByName(variant.Of)
	if err != nil {
		return fmt.Errorf("CLI '%s' is not installed, so there is no image to extend: %w", variant.Of, err)
	}

	buildFile := filepath.Join(projectDir, filepath.FromSlash(variant.BuildFile))
	if _, err := os.Stat(buildFile); err != nil {
		return fmt.Errorf("build file '%s' is not readable: %w", variant.BuildFile, err)
	}

	ref := variantImage(cli.Image, projectDir)
	oldID, _ := imageID(runtime, ref)
	fmt.Printf("Building '%s' from %s...\n", ref, variant.BuildFile)

	// --pull so an updated base in the registry is picked up. Without it the
	// build reuses the local image and silently stays on the old base.
	runtimeCmd := exec.Command(string(runtime), "build", "--pull", "-f", buildFile, "-t", ref, projectDir)
	runtimeCmd.Stdout = os.Stdout
	runtimeCmd.Stderr = os.Stderr
	if err := runtimeCmd.Run(); err != nil {
		return fmt.Errorf("failed to build with %s: %w", runtime, err)
	}

	fmt.Printf("Successfully built '%s'\n", ref)

	// The rebuild moved the tag, leaving the old image untagged. Remove it;
	// if it's still in use, rmi refuses and it simply stays.
	if newID, _ := imageID(runtime, ref); oldID != "" && newID != "" && newID != oldID {
		exec.Command(string(runtime), "rmi", oldID).Run()
	}

	if err := store.RecordVariantBuild(variant.Of, projectKey(projectDir), ref, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to record the variant build: %v\n", err)
	}
	return nil
}

// projectVariants returns the variants built for projectDir: the tracked
// ones, plus the declared ones whose image predates tracking.
func projectVariants(runtime container.Runtime, projectDir string) ([]models.TrackedVariant, error) {
	tracked, err := store.ListVariants("")
	if err != nil {
		return nil, err
	}
	project := projectKey(projectDir)
	var variants []models.TrackedVariant
	seen := map[string]bool{}
	for _, v := range tracked {
		if v.ProjectDir == project {
			variants = append(variants, v)
			seen[v.CLIName] = true
		}
	}

	declared, err := loadVariants(projectDir)
	if err != nil {
		return nil, err
	}
	for of := range declared {
		cli, err := store.GetCliByName(of)
		if seen[of] || err != nil {
			continue
		}
		ref := variantImage(cli.Image, projectDir)
		built, err := imageExists(runtime, ref)
		if err != nil {
			return nil, err
		}
		if built {
			variants = append(variants, models.TrackedVariant{CLIName: of, ProjectDir: project, Image: ref})
		}
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].CLIName < variants[j].CLIName })
	return variants, nil
}

var localVariantDestroyCmd = &cobra.Command{
	Use:     "destroy [cli]",
	Aliases: []string{"d"},
	Short:   "Remove this project's variant images",
	Long: `Remove this project's variant images, to get their disk space back.
With no argument every variant of this project is removed.

Each variant's image and dp's record of it are removed. ./.dp/variants.json
and the build files are left alone, so 'dp local variant build' brings it back.`,
	Example: `  dp local variant destroy
  dp local variant destroy claude`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeVariantNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err != nil {
			return err
		}
		variants, err := projectVariants(runtime, cwd)
		if err != nil {
			return err
		}

		// CLIs sharing an image share the variant tag; keep it if still used.
		var targets []models.TrackedVariant
		kept := map[string]bool{}
		for _, v := range variants {
			if len(args) == 0 || v.CLIName == args[0] {
				targets = append(targets, v)
			} else {
				kept[v.Image] = true
			}
		}
		if len(targets) == 0 {
			if len(args) == 1 {
				return fmt.Errorf("no variant of '%s' is built for this project", args[0])
			}
			fmt.Println("No variants built for this project.")
			return nil
		}

		var failed []string
		for _, v := range targets {
			if err := removeVariant(runtime, v, kept[v.Image]); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove variant of '%s': %v\n", v.CLIName, err)
				failed = append(failed, v.CLIName)
				continue
			}
			fmt.Printf("Removed variant of '%s' (%s)\n", v.CLIName, v.Image)
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed to destroy: %s", strings.Join(failed, ", "))
		}
		return nil
	},
}

var localVariantExtendCmd = &cobra.Command{
	Use:     "extend [cli]",
	Aliases: []string{"e"},
	Short:   "Declare a variant of an installed CLI for this project",
	Example: `  dp local variant extend claude`,
	Args:    cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		clis, err := store.ListCLIs()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, c := range clis {
			names = append(names, c.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := store.GetCliByName(args[0])
		if err != nil {
			return fmt.Errorf("failed to find CLI %s: %w", args[0], err)
		}
		// Load the current variants file
		variants, err := loadVariantsFile(".")
		if err != nil {
			return fmt.Errorf("failed to load variants file: %w", err)
		}
		// Overwriting would drop any local_mounts already set.
		if _, exists := variants.Variants[args[0]]; exists {
			return fmt.Errorf("a variant of '%s' is already declared in %s", args[0], variantsFile)
		}
		// Add the new variant with a default build file name
		buildFile := path.Join(variantBuildDir, args[0]+".Dockerfile")
		variants.Variants[args[0]] = models.FileVariant{
			BuildFile:   buildFile,
			LocalMounts: nil,
		}
		// Save the updated variants file
		data, err := json.MarshalIndent(variants, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal variants file: %w", err)
		}

		// Build file first: it's the half worth keeping if the next write fails.
		baseOS := ""
		runtime, err := container.ResolveRuntime(containerRuntimeFlag)
		if err == nil {
			baseOS, err = detectBaseOS(runtime, cli.Image)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not detect the base OS: %v\n", err)
		}
		created, err := writeVariantDockerfile(buildFile, cli.Image, baseOS)
		if err != nil {
			return err
		}

		// .dp does not exist yet for a project's first variant.
		if err := os.MkdirAll(filepath.Dir(variantsFile), 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", filepath.Dir(variantsFile), err)
		}
		err = os.WriteFile(variantsFile, data, 0644)
		if err != nil {
			return fmt.Errorf("failed to write variants file: %w", err)
		}
		if _, err := writeFileIfMissing(dpReadmeFile, dpReadme); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		fmt.Printf("Declared variant for CLI %s in %s\n", args[0], variantsFile)
		if created {
			fmt.Printf("Created %s (FROM %s) — add your changes there, then run 'dp local variant build %s'\n", buildFile, cli.Image, args[0])
		} else {
			fmt.Printf("Kept the existing %s\n", buildFile)
		}
		return nil
	},
}
