package cli

import (
	"fmt"

	"disguised-penguin/internal/models"

	"github.com/spf13/cobra"
)

func completeSettingKeys(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var keys []string
	for _, def := range models.SettingDefs() {
		keys = append(keys, fmt.Sprintf("%s\t%s", def.Key, def.Description))
	}
	return keys, cobra.ShellCompDirectiveNoFileComp
}

func lookupSetting(key string) (models.SettingDef, error) {
	def, ok := models.LookupSetting(key)
	if !ok {
		return def, fmt.Errorf("unknown setting '%s' (see 'dp settings list')", key)
	}
	return def, nil
}

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "View and change dp settings",
}

var settingsListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all settings with their current values",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, def := range models.SettingDefs() {
			value, err := store.GetSetting(def.Key)
			if err != nil {
				return err
			}
			fmt.Printf("%s = %s (%d)\n", def.Key, def.ValueName(value), value)
			fmt.Printf("    %s\n", def.Description)
			for _, v := range def.Values {
				fmt.Printf("    %d  %-6s %s\n", v.Value, v.Name, v.Description)
			}
		}
		return nil
	},
}

var settingsGetCmd = &cobra.Command{
	Use:               "get [key]",
	Short:             "Print the current value of a setting",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeSettingKeys,
	RunE: func(cmd *cobra.Command, args []string) error {
		def, err := lookupSetting(args[0])
		if err != nil {
			return err
		}
		value, err := store.GetSetting(def.Key)
		if err != nil {
			return err
		}
		fmt.Printf("%s (%d)\n", def.ValueName(value), value)
		return nil
	},
}

var settingsSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Change a setting (value can be its name or number)",
	Args:  cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeSettingKeys(cmd, args, toComplete)
		}
		def, ok := models.LookupSetting(args[0])
		if len(args) != 1 || !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var values []string
		for _, v := range def.Values {
			values = append(values, fmt.Sprintf("%s\t%s", v.Name, v.Description))
		}
		return values, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		def, err := lookupSetting(args[0])
		if err != nil {
			return err
		}
		value, err := def.Parse(args[1])
		if err != nil {
			return err
		}
		if err := store.SetSetting(def.Key, value); err != nil {
			return fmt.Errorf("failed to save setting: %w", err)
		}
		fmt.Printf("Set %s to %s (%d)\n", def.Key, def.ValueName(value), value)

		if def.Key == models.SettingAliasMode {
			fmt.Println("Aliases are loaded together with your shell completions: open a new shell to apply,")
			fmt.Println("and run 'dp install-completions' first if you haven't already.")
		}
		return nil
	},
}
