package container

import (
	"os"

	"github.com/mattn/go-isatty"
)

func isTerminal(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func StdioIsTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

// InteractiveFlags keeps -i so piped input still works, and adds -t only with a
// terminal: a pseudo-TTY on redirected stdout would merge stderr and emit CRLF.
func InteractiveFlags(tty bool) []string {
	if tty {
		return []string{"-i", "-t"}
	}
	return []string{"-i"}
}
