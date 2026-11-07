package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const maxJWKSBytes = 1 << 20

func newJWKSClient(iss Issuer) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if iss.CAFile != "" {
		pem, err := os.ReadFile(iss.CAFile)
		if err != nil {
			return nil, fmt.Errorf("auth issuer %s: read ca: %w", iss.Name, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("auth issuer %s: ca file has no PEM certificates", iss.Name)
		}
		tlsConfig.RootCAs = pool
	}

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = tlsConfig

	return &http.Client{
		Transport: jwksTransport{base: base, tokenFile: iss.TokenFile},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("jwks redirects are not followed")
		},
	}, nil
}

type jwksTransport struct {
	base      http.RoundTripper
	tokenFile string
}

// The token file is read on every fetch because Kubernetes rotates projected tokens in place.
func (t jwksTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.tokenFile != "" {
		token, err := os.ReadFile(t.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("read jwks token: %w", err)
		}
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = limitedBody{Reader: io.LimitReader(resp.Body, maxJWKSBytes), Closer: resp.Body}
	return resp, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}
