package kafka

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionValidate(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	srv.Close()
	caFile := filepath.Join(dir, "ca.crt")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	emptyCA := filepath.Join(dir, "empty.crt")
	os.WriteFile(emptyCA, []byte("not a certificate"), 0o600)

	brokers := []string{"b1:9093"}
	sasl := SASL{Mechanism: SASLScramSHA512, User: "risk", Password: "pw"}
	cases := map[string]struct {
		conn    Connection
		wantErr string
	}{
		"plaintext":             {Connection{Brokers: brokers, SASL: SASL{Mechanism: SASLNone}}, ""},
		"tls system roots":      {Connection{Brokers: brokers, TLS: TLS{Enabled: true}, SASL: SASL{Mechanism: SASLNone}}, ""},
		"tls custom ca + scram": {Connection{Brokers: brokers, TLS: TLS{Enabled: true, CAFile: caFile}, SASL: sasl}, ""},
		"no brokers":            {Connection{SASL: SASL{Mechanism: SASLNone}}, "brokers must be set"},
		"unknown mechanism":     {Connection{Brokers: brokers, SASL: SASL{Mechanism: "kerberos"}}, "sasl must be one of"},
		"sasl without tls":      {Connection{Brokers: brokers, SASL: sasl}, "sasl needs tls"},
		"sasl without user": {Connection{Brokers: brokers, TLS: TLS{Enabled: true},
			SASL: SASL{Mechanism: SASLPlain, Password: "pw"}}, "user must be set"},
		"sasl without password": {Connection{Brokers: brokers, TLS: TLS{Enabled: true},
			SASL: SASL{Mechanism: SASLPlain, User: "risk"}}, "password must be set"},
		"cert without key": {Connection{Brokers: brokers, TLS: TLS{Enabled: true, CertFile: "c.crt"},
			SASL: SASL{Mechanism: SASLNone}}, "cert and key must be set together"},
		"tls files with tls off": {Connection{Brokers: brokers, TLS: TLS{CAFile: caFile},
			SASL: SASL{Mechanism: SASLNone}}, "tls is off"},
		"missing ca": {Connection{Brokers: brokers, TLS: TLS{Enabled: true, CAFile: filepath.Join(dir, "nope")},
			SASL: SASL{Mechanism: SASLNone}}, "kafka tls ca"},
		"ca without certificates": {Connection{Brokers: brokers, TLS: TLS{Enabled: true, CAFile: emptyCA},
			SASL: SASL{Mechanism: SASLNone}}, "no certificates found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.conn.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("Validate() = %v, want it to mention %q", err, tc.wantErr)
			case err != nil && strings.Contains(err.Error(), "pw"):
				t.Errorf("error leaks the password: %v", err)
			}
		})
	}
}
