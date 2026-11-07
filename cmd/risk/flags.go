package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"risk/auth"
	"risk/config"
	"risk/db"
	"risk/kafka"
)

func addLogLevelFlag(fs *flag.FlagSet, level *slog.Level) {
	fs.TextVar(level, "log-level", slog.LevelInfo, "Lowest level logged (debug|info|warn|error)")
}

func addConfigFlag(fs *flag.FlagSet, path *string) {
	fs.StringVar(path, "config", "", "Path to the JSON config file; required")
}

func loadConfig(path string) (config.Config, error) {
	if path == "" {
		return config.Config{}, errors.New("set -config to the config file's path")
	}
	return config.Load(path)
}

func addDBFlags(fs *flag.FlagSet, o *db.Options) {
	fs.StringVar(&o.DSN, "db-dsn", os.Getenv("RISK_DB_DSN"), "PostgreSQL URL with sslmode=verify-full|verify-ca|disable")
	fs.IntVar(&o.MaxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	fs.IntVar(&o.MaxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	fs.DurationVar(&o.MaxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")
}

type kafkaFlags struct {
	conn kafka.Connection
	tls  onOff
}

func addKafkaFlags(fs *flag.FlagSet, k *kafkaFlags) {
	k.conn.Brokers = splitList(os.Getenv("RISK_KAFKA_BROKERS"))
	fs.Func("kafka-brokers", "Kafka seed brokers (comma separated)", func(val string) error {
		k.conn.Brokers = splitList(val)
		return nil
	})
	fs.Var(&k.tls, "kafka-tls", "TLS to Kafka: on or off; required")
	fs.StringVar(&k.conn.TLS.CAFile, "kafka-tls-ca", "", "CA file for Kafka's certificates ('' = system roots)")
	fs.StringVar(&k.conn.TLS.CertFile, "kafka-tls-cert", "", "Client certificate for mTLS to Kafka")
	fs.StringVar(&k.conn.TLS.KeyFile, "kafka-tls-key", "", "Client key for mTLS to Kafka")
	fs.StringVar(&k.conn.SASL.Mechanism, "kafka-sasl", kafka.SASLNone, "Kafka login (none|plain|scram-sha-256|scram-sha-512)")
	fs.StringVar(&k.conn.SASL.User, "kafka-sasl-user", "", "Kafka SASL user (the API key on Confluent Cloud)")
	fs.StringVar(&k.conn.SASL.Password, "kafka-sasl-password", os.Getenv("RISK_KAFKA_SASL_PASSWORD"), "Kafka SASL password")
}

func (k *kafkaFlags) connection() (kafka.Connection, error) {
	var errs []error
	if k.tls.set {
		k.conn.TLS.Enabled = k.tls.on
	} else {
		errs = append(errs, errors.New("set -kafka-tls to on or off"))
	}
	errs = append(errs, k.conn.Validate())
	return k.conn, errors.Join(errs...)
}

type httpTLSFlags struct {
	mode     onOff
	certFile string
	keyFile  string
}

type tlsFiles struct{ cert, key string }

func addHTTPTLSFlags(fs *flag.FlagSet, t *httpTLSFlags) {
	fs.Var(&t.mode, "http-tls", "TLS on the HTTP server: on or off (off when a mesh or gateway terminates TLS); required")
	fs.StringVar(&t.certFile, "http-tls-cert", "", "HTTP server certificate (PEM); reloaded when the file changes")
	fs.StringVar(&t.keyFile, "http-tls-key", "", "HTTP server private key (PEM); reloaded when the file changes")
}

// files is nil when TLS is off.
func (t *httpTLSFlags) files() (*tlsFiles, error) {
	switch {
	case !t.mode.set:
		return nil, errors.New("set -http-tls to on or off")
	case !t.mode.on && (t.certFile != "" || t.keyFile != ""):
		return nil, errors.New("-http-tls-cert and -http-tls-key have no effect with -http-tls off")
	case !t.mode.on:
		return nil, nil
	case t.certFile == "" || t.keyFile == "":
		return nil, errors.New("-http-tls on needs -http-tls-cert and -http-tls-key")
	}
	return &tlsFiles{cert: t.certFile, key: t.keyFile}, nil
}

type authFlags struct {
	mode    onOff
	issuers []auth.Issuer
	allow   []string
}

func addAuthFlags(fs *flag.FlagSet, a *authFlags) {
	fs.Var(&a.mode, "auth", "Bearer-token auth on the evaluated-txs API: on or off; required")
	fs.Func("auth-issuer", "Trusted token issuer, repeatable: name=,iss=,jwks=,aud=[,algs=RS256|ES256][,typ=][,ca=][,token=]",
		func(val string) error {
			iss, err := parseIssuer(val)
			if err != nil {
				return err
			}
			a.issuers = append(a.issuers, iss)
			return nil
		})
	fs.Func("auth-allow", "Caller allowed by -auth, repeatable: issuerName:subject or issuerName:*", func(val string) error {
		a.allow = append(a.allow, val)
		return nil
	})
}

// config is nil when auth is off.
func (a *authFlags) config() (*auth.Config, error) {
	switch {
	case !a.mode.set:
		return nil, errors.New("set -auth to on or off")
	case !a.mode.on && (len(a.issuers) > 0 || len(a.allow) > 0):
		return nil, errors.New("-auth-issuer and -auth-allow have no effect with -auth off")
	case !a.mode.on:
		return nil, nil
	}
	cfg := &auth.Config{Issuers: a.issuers, Allow: a.allow}
	return cfg, cfg.Validate()
}

func parseIssuer(val string) (auth.Issuer, error) {
	iss := auth.Issuer{Algorithms: auth.DefaultAlgorithms}
	for pair := range strings.SplitSeq(val, ",") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return auth.Issuer{}, fmt.Errorf("%q is not key=value", pair)
		}
		switch key {
		case "name":
			iss.Name = value
		case "iss":
			iss.Issuer = value
		case "jwks":
			iss.JWKSURL = value
		case "aud":
			iss.Audience = value
		case "algs":
			iss.Algorithms = strings.Split(value, "|")
		case "typ":
			iss.Type = value
		case "ca":
			iss.CAFile = value
		case "token":
			iss.TokenFile = value
		default:
			return auth.Issuer{}, fmt.Errorf("unknown key %q; use name, iss, jwks, aud, algs, typ, ca or token", key)
		}
	}
	return iss, nil
}

type onOff struct {
	set bool
	on  bool
}

func (o *onOff) String() string {
	switch {
	case !o.set:
		return ""
	case o.on:
		return "on"
	default:
		return "off"
	}
}

func (o *onOff) Set(val string) error {
	switch val {
	case "on":
		o.on = true
	case "off":
		o.on = false
	default:
		return errors.New("must be on or off")
	}
	o.set = true
	return nil
}

func splitList(val string) []string {
	return strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' })
}
