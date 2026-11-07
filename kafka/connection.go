package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

const (
	SASLNone        = "none"
	SASLPlain       = "plain"
	SASLScramSHA256 = "scram-sha-256"
	SASLScramSHA512 = "scram-sha-512"
)

var saslMechanisms = []string{SASLNone, SASLPlain, SASLScramSHA256, SASLScramSHA512}

// Connection is how every client in the service reaches Kafka, so consumer and publisher can't drift apart.
type Connection struct {
	Brokers []string
	TLS     TLS
	SASL    SASL
}

type TLS struct {
	Enabled  bool
	CAFile   string // empty uses the system roots
	CertFile string
	KeyFile  string
}

type SASL struct {
	Mechanism string
	User      string
	Password  string
}

func (c Connection) Validate() error {
	var errs []error
	require := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}

	require(len(c.Brokers) > 0, "kafka brokers must be set")

	require(slices.Contains(saslMechanisms, c.SASL.Mechanism), fmt.Sprintf("kafka sasl must be one of %v", saslMechanisms))
	if c.SASL.Mechanism != SASLNone {
		require(c.TLS.Enabled, "kafka sasl needs tls, or credentials are sent in the clear")
		require(c.SASL.User != "", "kafka sasl user must be set")
		require(c.SASL.Password != "", "kafka sasl password must be set")
	}

	require((c.TLS.CertFile == "") == (c.TLS.KeyFile == ""), "kafka tls cert and key must be set together")
	if c.TLS.Enabled {
		if _, err := c.TLS.config(); err != nil {
			errs = append(errs, err)
		}
	} else {
		require(c.TLS.CAFile == "" && c.TLS.CertFile == "", "kafka tls files are set but tls is off")
	}

	return errors.Join(errs...)
}

func (c Connection) options() ([]kgo.Opt, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(c.Brokers...)}

	if c.TLS.Enabled {
		tlsCfg, err := c.TLS.config()
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
	}

	if m := c.SASL.mechanism(); m != nil {
		opts = append(opts, kgo.SASL(m))
	}
	return opts, nil
}

// franz-go sets ServerName to each broker's host, so certificates are verified per broker.
func (t TLS) config() (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("kafka tls ca: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("kafka tls ca %s: no certificates found", t.CAFile)
		}
	}

	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("kafka tls client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func (s SASL) mechanism() sasl.Mechanism {
	switch s.Mechanism {
	case SASLPlain:
		return plain.Auth{User: s.User, Pass: s.Password}.AsMechanism()
	case SASLScramSHA256:
		return scram.Auth{User: s.User, Pass: s.Password}.AsSha256Mechanism()
	case SASLScramSHA512:
		return scram.Auth{User: s.User, Pass: s.Password}.AsSha512Mechanism()
	default:
		return nil
	}
}
