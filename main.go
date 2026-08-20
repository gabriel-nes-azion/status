// Command status is a terminal dashboard that plots local system resources and
// the latency of a remote endpoint on shared ASCII timelines, driven by
// slash commands typed inside the TUI.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"status/internal/config"
	"status/internal/ui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// A broken config file should not stop the dashboard from starting.
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", err)
	}

	if len(os.Args) > 1 {
		if err := applyArgs(cfg, os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			usage()
			os.Exit(2)
		}
	}

	p := tea.NewProgram(ui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func applyArgs(cfg *config.Config, args []string) error {
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			usage()
			os.Exit(0)
		default:
			return fmt.Errorf("unknown argument %q", a)
		}
	}
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `status — terminal monitoring dashboard

usage: status

Everything is configured from inside the TUI with slash commands; type /help
once it is running. Configuration is read from and written to:
  `+config.FilePath()+`
`)
}
