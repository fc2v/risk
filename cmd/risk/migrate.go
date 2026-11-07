package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"

	"risk/db"
	"risk/migrations"
)

const migrateUsage = `usage: risk migrate <action> [flags]

actions:
  up        apply every pending migration
  version   show the applied version and whether the last migration was left half-done
  down      roll back -steps migrations
  force     mark -version as cleanly applied, after repairing a half-done migration by hand

"risk migrate <action> -h" lists an action's flags.
`

func migrateCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, migrateUsage)
		return 2
	}
	action, args := args[0], args[1:]

	var (
		logLevel slog.Level
		dbOpts   db.Options
		steps    int
		version  int
	)
	fs := flag.NewFlagSet("migrate "+action, flag.ExitOnError)
	addLogLevelFlag(fs, &logLevel)
	addDBFlags(fs, &dbOpts)
	switch action {
	case "up", "version":
	case "down":
		fs.IntVar(&steps, "steps", 0, "Number of migrations to roll back; required")
	case "force":
		fs.IntVar(&version, "version", -1, "Version to mark as cleanly applied; required")
	default:
		fmt.Fprintf(os.Stderr, "unknown migrate action %q\n\n%s", action, migrateUsage)
		return 2
	}
	fs.Parse(args)

	logger := newLogger(logLevel)
	if err := runMigrate(action, dbOpts, steps, version, logger); err != nil {
		logger.Error("migrate failed", "action", action, "err", err)
		return 1
	}
	return 0
}

func runMigrate(action string, o db.Options, steps, version int, logger *slog.Logger) error {
	switch {
	case action == "down" && steps < 1:
		return errors.New("set -steps to how many migrations to roll back")
	case action == "force" && version < 0:
		return errors.New("set -version to the version to mark as applied")
	}

	sqlDB, err := db.Open(o)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	m, err := newMigrator(sqlDB, logger)
	if err != nil {
		return err
	}
	defer m.Close()

	switch action {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-steps)
	case "force":
		err = m.Force(version)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	current, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		current, err = 0, nil
	}
	if err != nil {
		return err
	}
	logger.Info("migrations", "action", action, "schemaVersion", current, "dirty", dirty)
	return nil
}

func newMigrator(sqlDB *sql.DB, logger *slog.Logger) (*migrate.Migrate, error) {
	m, err := migrations.New(sqlDB)
	if err != nil {
		return nil, err
	}
	m.Log = migrateLog{logger}
	return m, nil
}

type migrateLog struct{ logger *slog.Logger }

func (l migrateLog) Printf(format string, v ...any) {
	l.logger.Info(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

func (migrateLog) Verbose() bool { return false }
