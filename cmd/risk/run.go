package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"os/signal"
	"syscall"
	"time"

	"risk/auth"
	"risk/config"
	"risk/db"
	"risk/kafka"
	"risk/metrics"
	"risk/risk/consumer"
	"risk/state"
)

const kafkaPingTimeout = 5 * time.Second

type runFlags struct {
	logLevel   slog.Level
	configPath string
	httpAddr   string
	db         db.Options
	kafka      kafkaFlags
	auth       authFlags
	httpTLS    httpTLSFlags
}

func runCommand(args []string) int {
	var f runFlags
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	addLogLevelFlag(fs, &f.logLevel)
	addConfigFlag(fs, &f.configPath)
	fs.StringVar(&f.httpAddr, "http-addr", ":4000", "Address the HTTP server listens on (e.g. 127.0.0.1:4000 behind a local proxy)")
	addDBFlags(fs, &f.db)
	addKafkaFlags(fs, &f.kafka)
	addAuthFlags(fs, &f.auth)
	addHTTPTLSFlags(fs, &f.httpTLS)
	fs.Parse(args)

	logger := newLogger(f.logLevel)
	if err := run(f, logger); err != nil {
		logger.Error("exiting", "err", err)
		return 1
	}
	logger.Info("stopped")
	return 0
}

func run(f runFlags, logger *slog.Logger) error {
	cfg, conn, authCfg, certFiles, err := checkRunFlags(f)
	if err != nil {
		return err
	}

	sqlDB, err := db.Open(f.db)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	m := metrics.New()
	m.WatchDB(sqlDB)

	publisher, err := kafka.NewPublisher(conn)
	if err != nil {
		return fmt.Errorf("kafka publisher: %w", err)
	}
	defer publisher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), kafkaPingTimeout)
	defer cancel()
	if err := publisher.Ping(ctx); err != nil {
		return fmt.Errorf("ping kafka: %w", err)
	}

	httpAPI := &api{
		logger:       logger,
		metrics:      m,
		evaluatedTxs: db.EvaluatedTxModel{DB: sqlDB},
		readinessChecks: map[string]func(context.Context) error{
			"db":    sqlDB.PingContext,
			"kafka": publisher.Ping,
		},
	}
	if authCfg != nil {
		authCtx, stopAuth := context.WithCancel(context.Background())
		defer stopAuth()
		verifier, err := auth.NewVerifier(authCtx, *authCfg, logger)
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		httpAPI.verifier = verifier
		maps.Copy(httpAPI.readinessChecks, verifier.ReadinessChecks())
	}
	var tlsConfig *tls.Config
	if certFiles != nil {
		certs, err := newCertReloader(certFiles.cert, certFiles.key, logger)
		if err != nil {
			return fmt.Errorf("http tls: %w", err)
		}
		tlsConfig = serverTLSConfig(certs)
	}
	stopHTTP, err := httpAPI.serve(f.httpAddr, tlsConfig)
	if err != nil {
		return err
	}
	// Deferred before the consumer so HTTP stays up until the consumer has finished.
	defer stopHTTP()

	backends, err := state.NewBackends(cfg, logger, sqlDB, publisher, m)
	if err != nil {
		return err
	}
	handler := consumer.NewConsumer(backends)
	kafkaConsumer, err := kafka.NewConsumer(conn, cfg.Consumer, handler, sqlDB.PingContext, m, logger)
	if err != nil {
		return fmt.Errorf("kafka consumer: %w", err)
	}
	// Closing leaves the group, which stops the workers and commits what they settled.
	defer kafkaConsumer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sslMode, _ := f.db.SSLMode()
	logger.Info("starting",
		"buildTime", buildTime,
		"httpAddr", f.httpAddr,
		"auth", f.auth.mode.String(),
		"httpTLS", f.httpTLS.mode.String(),
		"dbSSLMode", sslMode,
		"kafkaTLS", conn.TLS.Enabled,
		"kafkaSASL", conn.SASL.Mechanism,
		"kafkaBrokers", len(conn.Brokers),
		"topic", cfg.Consumer.Topic,
		"group", cfg.Consumer.Group)

	if err := kafkaConsumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutting down")
	return nil
}

// checkRunFlags reports every problem at once, before anything connects.
func checkRunFlags(f runFlags) (config.Config, kafka.Connection, *auth.Config, *tlsFiles, error) {
	cfg, cfgErr := loadConfig(f.configPath)
	conn, kafkaErr := f.kafka.connection()
	authCfg, authErr := f.auth.config()
	certFiles, tlsErr := f.httpTLS.files()
	return cfg, conn, authCfg, certFiles, errors.Join(cfgErr, f.db.Validate(), kafkaErr, authErr, tlsErr)
}
