package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"risk/db"
)

const profilesUsage = `usage: risk profiles rebuild [flags]

Recomputes account profiles from evaluated transactions, e.g. after a rule or lookback change.
Evaluations wait while it runs.
`

func profilesCommand(args []string) int {
	if len(args) == 0 || args[0] != "rebuild" {
		fmt.Fprint(os.Stderr, profilesUsage)
		return 2
	}

	var (
		logLevel slog.Level
		dbOpts   db.Options
	)
	fs := flag.NewFlagSet("profiles rebuild", flag.ExitOnError)
	addLogLevelFlag(fs, &logLevel)
	addDBFlags(fs, &dbOpts)
	fs.Parse(args[1:])

	logger := newLogger(logLevel)
	sqlDB, err := db.Open(dbOpts)
	if err != nil {
		logger.Error("profiles rebuild failed", "err", err)
		return 1
	}
	defer sqlDB.Close()

	counts, err := db.RebuildProfiles(context.Background(), sqlDB)
	if err != nil {
		logger.Error("profiles rebuild failed", "err", err)
		return 1
	}
	logger.Info("profiles rebuilt", "hourRows", counts.Hours, "seenRows", counts.Seen)
	return 0
}
