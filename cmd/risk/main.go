package main

import (
	"fmt"
	"log/slog"
	"os"
)

const (
	app     = "risk"
	version = "1.0.0"
)

var buildTime string

const usage = `usage: risk <command> [flags]

commands:
  run       consume txs from Kafka, evaluate and publish them, and serve HTTP
  migrate   apply or inspect database migrations
  profiles  rebuild account profiles from evaluated transactions
  version   print the version and build time

"risk <command> -h" lists a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch command, args := os.Args[1], os.Args[2:]; command {
	case "run":
		os.Exit(runCommand(args))
	case "migrate":
		os.Exit(migrateCommand(args))
	case "profiles":
		os.Exit(profilesCommand(args))
	case "version":
		fmt.Printf("version:\t%s\n", version)
		fmt.Printf("Build time:\t%s\n", buildTime)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}
}

func newLogger(level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	})
	return slog.New(handler).With("app", app, "version", version)
}
