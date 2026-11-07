package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	clusterIss = "https://kubernetes.default.svc"
	idpIss     = "https://idp.example.com/realms/prod"
	caseMgr    = "system:serviceaccount:payments:case-manager"
	jwksToken  = "jwks-reader-token"
)

type testEnv struct {
	srv       *httptest.Server
	cfg       Config
	rsaKey    *rsa.PrivateKey
	ecKey     *ecdsa.PrivateKey
	fetches   atomic.Int64
	mu        sync.Mutex
	clusterJW []map[string]any
	override  http.HandlerFunc
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{rsaKey: mustRSA(t), ecKey: mustEC(t)}
	env.clusterJW = []map[string]any{rsaJWK("rsa-1", env.rsaKey)}

	env.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.fetches.Add(1)
		env.mu.Lock()
		override, keys := env.override, env.clusterJW
		env.mu.Unlock()
		if override != nil {
			override(w, r)
			return
		}
		switch r.URL.Path {
		case "/cluster":
			if r.Header.Get("Authorization") != "Bearer "+jwksToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"keys": keys})
		case "/idp":
			json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{ecJWK("ec-1", env.ecKey)}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(env.srv.Close)

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	tokenFile := filepath.Join(dir, "token")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: env.srv.Certificate().Raw}), 0o600)
	os.WriteFile(tokenFile, []byte(jwksToken+"\n"), 0o600)

	env.cfg = Config{
		Issuers: []Issuer{
			{Name: "cluster", Issuer: clusterIss, JWKSURL: env.srv.URL + "/cluster", Audience: "risk",
				Algorithms: DefaultAlgorithms, CAFile: caFile, TokenFile: tokenFile},
			{Name: "idp", Issuer: idpIss, JWKSURL: env.srv.URL + "/idp", Audience: "risk-api",
				Algorithms: []string{"ES256"}, Type: "at+jwt", CAFile: caFile},
		},
		Allow: []string{"cluster:" + caseMgr, "idp:*"},
	}
	return env
}

func (env *testEnv) verifier(t *testing.T) *Verifier {
	t.Helper()
	v, err := NewVerifier(t.Context(), env.cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func claims(iss, aud, sub string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": iss, "aud": aud, "sub": sub,
		"iat": now.Unix(), "nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

func sign(t *testing.T, method jwt.SigningMethod, key any, kid string, c jwt.MapClaims, header map[string]any) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	maps.Copy(tok.Header, header)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (env *testEnv) clusterToken(t *testing.T, c jwt.MapClaims) string {
	return sign(t, jwt.SigningMethodRS256, env.rsaKey, "rsa-1", c, nil)
}

func (env *testEnv) idpToken(t *testing.T, c jwt.MapClaims) string {
	return sign(t, jwt.SigningMethodES256, env.ecKey, "ec-1", c, map[string]any{"typ": "at+jwt"})
}

func TestVerify(t *testing.T) {
	env := newTestEnv(t)
	v := env.verifier(t)

	with := func(c jwt.MapClaims, k string, val any) jwt.MapClaims {
		c[k] = val
		return c
	}
	without := func(c jwt.MapClaims, k string) jwt.MapClaims {
		delete(c, k)
		return c
	}
	clusterOK := func() jwt.MapClaims { return claims(clusterIss, "risk", caseMgr) }
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustMarshalPub(t, &env.rsaKey.PublicKey)})
	valid := env.clusterToken(t, clusterOK())
	other := env.clusterToken(t, with(clusterOK(), "sub", "someone-else"))

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"cluster RS256 allowed subject", valid, nil},
		{"idp ES256 any subject", env.idpToken(t, claims(idpIss, "risk-api", "fraud-ui")), nil},
		{"aud as a list", env.clusterToken(t, with(clusterOK(), "aud", []string{"other", "risk"})), nil},
		{"expired", env.clusterToken(t, with(clusterOK(), "exp", time.Now().Add(-time.Hour).Unix())), ErrUnauthenticated},
		{"expiry within leeway", env.clusterToken(t, with(clusterOK(), "exp", time.Now().Add(-10*time.Second).Unix())), nil},
		{"no exp", env.clusterToken(t, without(clusterOK(), "exp")), ErrUnauthenticated},
		{"not yet valid", env.clusterToken(t, with(clusterOK(), "nbf", time.Now().Add(time.Hour).Unix())), ErrUnauthenticated},
		{"untrusted issuer", env.clusterToken(t, with(clusterOK(), "iss", "https://evil.example.com")), ErrUnauthenticated},
		{"issuer differs in case", env.clusterToken(t, with(clusterOK(), "iss", strings.ToUpper(clusterIss))), ErrUnauthenticated},
		{"wrong audience", env.clusterToken(t, with(clusterOK(), "aud", "billing")), ErrUnauthenticated},
		{"no audience", env.clusterToken(t, without(clusterOK(), "aud")), ErrUnauthenticated},
		{"alg none", sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, "rsa-1", clusterOK(), nil), ErrUnauthenticated},
		{"HS256 signed with the public key", sign(t, jwt.SigningMethodHS256, pubPEM, "rsa-1", clusterOK(), nil), ErrUnauthenticated},
		{"alg not allowed for issuer", sign(t, jwt.SigningMethodRS256, env.rsaKey, "ec-1", claims(idpIss, "risk-api", "x"), map[string]any{"typ": "at+jwt"}), ErrUnauthenticated},
		{"key type doesn't match alg", sign(t, jwt.SigningMethodES256, env.ecKey, "rsa-1", clusterOK(), nil), ErrUnauthenticated},
		{"no kid", sign(t, jwt.SigningMethodRS256, env.rsaKey, "", clusterOK(), nil), ErrUnauthenticated},
		{"signed by another key", sign(t, jwt.SigningMethodRS256, mustRSA(t), "rsa-1", clusterOK(), nil), ErrUnauthenticated},
		{"tampered payload", splice(valid, other), ErrUnauthenticated},
		{"malformed", "not.a.jwt", ErrUnauthenticated},
		{"no sub", env.clusterToken(t, without(clusterOK(), "sub")), ErrUnauthenticated},
		{"typ missing", sign(t, jwt.SigningMethodES256, env.ecKey, "ec-1", claims(idpIss, "risk-api", "x"), map[string]any{"typ": nil}), ErrUnauthenticated},
		{"typ wrong", sign(t, jwt.SigningMethodES256, env.ecKey, "ec-1", claims(idpIss, "risk-api", "x"), map[string]any{"typ": "JWT"}), ErrUnauthenticated},
		{"subject not allowed", other, ErrForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(t.Context(), tc.token)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Errorf("Verify() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifyIdentity(t *testing.T) {
	env := newTestEnv(t)
	id, err := env.verifier(t).Verify(t.Context(), env.clusterToken(t, claims(clusterIss, "risk", caseMgr)))
	if err != nil || id != (Identity{Issuer: "cluster", Subject: caseMgr}) {
		t.Errorf("Verify() = %+v, %v", id, err)
	}
}

func TestKeyRotationRefreshesOnUnknownKID(t *testing.T) {
	env := newTestEnv(t)
	v := env.verifier(t)

	rotated := mustRSA(t)
	env.mu.Lock()
	env.clusterJW = append(env.clusterJW, rsaJWK("rsa-2", rotated))
	env.mu.Unlock()

	token := sign(t, jwt.SigningMethodRS256, rotated, "rsa-2", claims(clusterIss, "risk", caseMgr), nil)
	if _, err := v.Verify(t.Context(), token); err != nil {
		t.Fatalf("token signed with a newly published key: %v", err)
	}
}

func TestUnknownKIDFloodFetchesAtMostOnce(t *testing.T) {
	env := newTestEnv(t)
	v := env.verifier(t)
	before := env.fetches.Load()

	start := time.Now()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			token := sign(t, jwt.SigningMethodRS256, env.rsaKey, fmt.Sprintf("forged-%d", i), claims(clusterIss, "risk", caseMgr), nil)
			if _, err := v.Verify(t.Context(), token); !errors.Is(err, ErrUnauthenticated) {
				t.Errorf("forged kid accepted: %v", err)
			}
		})
	}
	wg.Wait()

	if extra := env.fetches.Load() - before; extra > 1 {
		t.Errorf("100 unknown kids caused %d jwks fetches, want at most 1", extra)
	}
	if took := time.Since(start); took > 2*fetchTimeout {
		t.Errorf("unknown kids took %s to reject; requests should not queue on the rate limit", took)
	}
}

func TestReadiness(t *testing.T) {
	t.Run("keys loaded", func(t *testing.T) {
		env := newTestEnv(t)
		checks := env.verifier(t).ReadinessChecks()
		for _, name := range []string{"jwks:cluster", "jwks:idp"} {
			if err := checks[name](t.Context()); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	})

	for name, handler := range map[string]http.HandlerFunc{
		"oversized key set": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"keys":[],"padding":"%s"}`, strings.Repeat("x", maxJWKSBytes))
		},
		"redirected": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example.com/jwks", http.StatusFound)
		},
		"missing jwks token": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.override = handler
			if err := env.verifier(t).ReadinessChecks()["jwks:cluster"](t.Context()); err == nil {
				t.Error("ready without keys")
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	base := func() Config {
		return Config{
			Issuers: []Issuer{{Name: "idp", Issuer: idpIss, JWKSURL: "https://idp.example.com/jwks", Audience: "risk", Algorithms: DefaultAlgorithms}},
			Allow:   []string{"idp:*"},
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	cases := map[string]func(*Config){
		"no issuers":            func(c *Config) { c.Issuers = nil; c.Allow = nil },
		"http jwks":             func(c *Config) { c.Issuers[0].JWKSURL = "http://idp.example.com/jwks" },
		"symmetric alg":         func(c *Config) { c.Issuers[0].Algorithms = []string{"HS256"} },
		"alg none":              func(c *Config) { c.Issuers[0].Algorithms = []string{"none"} },
		"no audience":           func(c *Config) { c.Issuers[0].Audience = "" },
		"colon in name":         func(c *Config) { c.Issuers[0].Name = "i:dp"; c.Allow = []string{"i:dp:*"} },
		"no allow entry":        func(c *Config) { c.Allow = nil },
		"allow unknown issuer":  func(c *Config) { c.Allow = append(c.Allow, "other:svc") },
		"allow without subject": func(c *Config) { c.Allow = []string{"idp:"} },
		"duplicate name":        func(c *Config) { c.Issuers = append(c.Issuers, c.Issuers[0]) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base()
			mutate(&c)
			if c.Validate() == nil {
				t.Error("accepted")
			}
		})
	}
}

func splice(a, b string) string {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	return pa[0] + "." + pb[1] + "." + pa[2]
}

func mustRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustEC(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustMarshalPub(t *testing.T, pub any) []byte {
	t.Helper()
	b, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func rsaJWK(kid string, k *rsa.PrivateKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
	}
}

func ecJWK(kid string, k *ecdsa.PrivateKey) map[string]any {
	pub, _ := k.PublicKey.ECDH()
	raw := pub.Bytes()
	return map[string]any{
		"kty": "EC", "kid": kid, "use": "sig", "alg": "ES256", "crv": "P-256",
		"x": b64(raw[1:33]), "y": b64(raw[33:]),
	}
}
