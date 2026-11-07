package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"

	_ "github.com/lib/pq"
)

const pingTimeout = 5 * time.Second

// require, prefer and allow encrypt without verifying the server, so they can't detect impersonation.
var allowedSSLModes = []string{"verify-full", "verify-ca", "disable"}

type Options struct {
	DSN          string
	MaxOpenConns int
	MaxIdleConns int
	MaxIdleTime  time.Duration
}

// Errors never include the DSN, since it carries the password.
func (o Options) Validate() error {
	var errs []error
	require := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}

	if o.DSN == "" {
		errs = append(errs, errors.New("db dsn must be set"))
	} else if _, err := o.SSLMode(); err != nil {
		errs = append(errs, err)
	}

	require(o.MaxOpenConns >= 1, "db max open conns must be >= 1")
	require(o.MaxIdleConns >= 0, "db max idle conns must be >= 0")
	require(o.MaxIdleTime >= 0, "db max idle time must be >= 0")

	return errors.Join(errs...)
}

func (o Options) SSLMode() (string, error) {
	u, err := url.Parse(o.DSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", errors.New("db dsn must be a postgres:// URL")
	}

	mode := u.Query().Get("sslmode")
	if !slices.Contains(allowedSSLModes, mode) {
		return "", fmt.Errorf("db dsn sslmode must be one of %v", allowedSSLModes)
	}
	return mode, nil
}

func Open(o Options) (*sql.DB, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", o.DSN)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(o.MaxOpenConns)
	db.SetMaxIdleConns(o.MaxIdleConns)
	db.SetConnMaxIdleTime(o.MaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}
