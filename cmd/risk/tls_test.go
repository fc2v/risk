package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCert(t *testing.T, files tlsFiles, name string, modTime time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, files.cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), modTime)
	writeFile(t, files.key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), modTime)

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func writeFile(t *testing.T, path string, data []byte, modTime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func tempCertFiles(t *testing.T) tlsFiles {
	dir := t.TempDir()
	return tlsFiles{cert: filepath.Join(dir, "tls.crt"), key: filepath.Join(dir, "tls.key")}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func servedName(t *testing.T, r *certReloader) string {
	t.Helper()
	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.Subject.CommonName
}

func TestCertReloader(t *testing.T) {
	files := tempCertFiles(t)
	start := time.Now().Add(-time.Hour)
	writeCert(t, files, "first", start)

	r, err := newCertReloader(files.cert, files.key, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if got := servedName(t, r); got != "first" {
		t.Fatalf("serving %q, want first", got)
	}

	writeCert(t, files, "renewed", start.Add(time.Minute))
	if got := servedName(t, r); got != "renewed" {
		t.Errorf("after renewal serving %q, want renewed", got)
	}

	writeFile(t, files.cert, []byte("not a certificate"), start.Add(2*time.Minute))
	if got := servedName(t, r); got != "renewed" {
		t.Errorf("after a broken renewal serving %q, want the previous certificate", got)
	}

	if _, err := newCertReloader(filepath.Join(t.TempDir(), "missing.crt"), files.key, discardLogger()); err == nil {
		t.Error("missing certificate accepted at startup")
	}
}

func TestServeTLS(t *testing.T) {
	files := tempCertFiles(t)
	leaf := writeCert(t, files, "risk", time.Now())
	r, err := newCertReloader(files.cert, files.key, discardLogger())
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	stop, err := testAPI(nil).serve(addr, serverTLSConfig(r))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots},
		ForceAttemptHTTP2: true,
	}}

	resp, err := client.Get("https://" + addr + "/v1/healthcheck")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ProtoMajor != 2 {
		t.Errorf("https healthcheck = %d over %s, want 200 over HTTP/2", resp.StatusCode, resp.Proto)
	}

	plain, err := http.Get("http://" + addr + "/v1/healthcheck")
	if err != nil {
		t.Fatal(err)
	}
	plain.Body.Close()
	if plain.StatusCode != http.StatusBadRequest {
		t.Errorf("plain http to the tls port = %d, want 400", plain.StatusCode)
	}
}

func TestHTTPTLSFlags(t *testing.T) {
	on, off := onOff{set: true, on: true}, onOff{set: true}
	cases := []struct {
		name    string
		flags   httpTLSFlags
		wantErr bool
		wantOn  bool
	}{
		{"unset", httpTLSFlags{}, true, false},
		{"off", httpTLSFlags{mode: off}, false, false},
		{"off with files", httpTLSFlags{mode: off, certFile: "c", keyFile: "k"}, true, false},
		{"on without key", httpTLSFlags{mode: on, certFile: "c"}, true, false},
		{"on with files", httpTLSFlags{mode: on, certFile: "c", keyFile: "k"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := tc.flags.files()
			if (err != nil) != tc.wantErr || (files != nil) != tc.wantOn {
				t.Errorf("files() = %v, %v; want error %v, on %v", files, err, tc.wantErr, tc.wantOn)
			}
		})
	}
}
