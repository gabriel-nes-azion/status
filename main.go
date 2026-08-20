// Command status is a terminal dashboard that plots local system resources, the
// latency of a remote endpoint and the CPU share of the services running on the
// machine on shared ASCII timelines, driven by slash commands typed inside the
// TUI.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"status/internal/config"
	"status/internal/ui"
)

// version identifies the build. The release target sets it:
//
//	go build -ldflags "-X main.version=$(VERSION)"
//
// A binary handed to someone else has to be able to say which build it is.
var version = "dev"

// options are the command line, which only decides where the configuration
// comes from and whether it is written back. Everything else is a slash command.
type options struct {
	confPath string
	noSave   bool
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		usage()
		os.Exit(2)
	}

	cfg, err := config.Load(opts.confPath)
	if err != nil {
		// A broken or unreachable config file should not stop the dashboard from
		// starting; it starts on the defaults and says why.
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if from := cfg.MigratedFrom(); from != "" {
		fmt.Fprintf(os.Stderr, "note: imported settings from %s; %s is the config file from now on\n",
			from, cfg.Path())
	}

	p := tea.NewProgram(ui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// The session's settings are written back on the way out, so the next run
	// starts where this one left off. Nothing is written when nothing changed.
	if !opts.noSave {
		path, err := cfg.SaveIfChanged()
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "warning: could not save %s: %v\n", cfg.Path(), err)
			os.Exit(1)
		case path != "":
			if backup := cfg.SavedBackup(); backup != "" {
				fmt.Fprintf(os.Stderr, "note: %s could not be read; a copy of it is at %s\n",
					cfg.Path(), backup)
			}
			fmt.Fprintln(os.Stderr, "saved", path)
		}
	}
}

func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			usage()
			os.Exit(0)

		case a == "-v" || a == "--version" || a == "version":
			fmt.Println(config.AppName, version)
			os.Exit(0)

		case a == "--no-save":
			opts.noSave = true

		case a == "-c" || a == "--conf" || a == "--config":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a file path", a)
			}
			i++
			opts.confPath = args[i]

		case strings.HasPrefix(a, "--conf=") || strings.HasPrefix(a, "--config="):
			_, value, _ := strings.Cut(a, "=")
			if value == "" {
				return opts, fmt.Errorf("%s needs a file path", a)
			}
			opts.confPath = value

		default:
			return opts, fmt.Errorf("unknown argument %q", a)
		}
	}
	return opts, nil
}

func usage() {
	fmt.Fprint(os.Stderr, `status `+version+` — terminal monitoring dashboard

usage: status [--conf <file.toml>] [--no-save]

  -c, --conf <file>  read and write this configuration instead of the default,
                     which is how you keep one setup per environment. A file
                     that does not exist yet is created on exit.
      --no-save      do not write the configuration back on exit
  -v, --version      print the version
  -h, --help         print this

Everything else is configured from inside the TUI with slash commands; type
/help once it is running. Settings are written back on exit and by /save.

Default configuration file:
  `+config.DefaultPath()+`
`)
}
