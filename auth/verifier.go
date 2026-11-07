package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

const (
	refreshInterval      = 15 * time.Minute
	unknownKIDRefreshGap = time.Minute
	fetchTimeout         = 5 * time.Second
	leeway               = 30 * time.Second
)

type Identity struct {
	Issuer  string
	Subject string
}

type Verifier struct {
	byIss map[string]*issuer
	allow map[string]map[string]bool
}

type issuer struct {
	cfg    Issuer
	keys   keyfunc.Keyfunc
	parser *jwt.Parser
}

// Key sets refresh in the background until ctx ends.
func NewVerifier(ctx context.Context, cfg Config, logger *slog.Logger) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	v := &Verifier{byIss: map[string]*issuer{}, allow: map[string]map[string]bool{}}
	for _, iss := range cfg.Issuers {
		client, err := newJWKSClient(iss)
		if err != nil {
			return nil, err
		}
		keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{iss.JWKSURL}, keyfunc.Override{
			Client:            client,
			HTTPTimeout:       fetchTimeout,
			RefreshInterval:   refreshInterval,
			RefreshUnknownKID: rate.NewLimiter(rate.Every(unknownKIDRefreshGap), 1),
			RateLimitWaitMax:  fetchTimeout,
			RefreshErrorHandlerFunc: func(string) func(context.Context, error) {
				return func(_ context.Context, err error) {
					logger.Warn("jwks refresh failed", "issuer", iss.Name, "err", err)
				}
			},
		})
		if err != nil {
			return nil, fmt.Errorf("auth issuer %s: %w", iss.Name, err)
		}

		v.byIss[iss.Issuer] = &issuer{
			cfg:  iss,
			keys: keys,
			parser: jwt.NewParser(
				jwt.WithValidMethods(iss.Algorithms),
				jwt.WithIssuer(iss.Issuer),
				jwt.WithAudience(iss.Audience),
				jwt.WithExpirationRequired(),
				jwt.WithLeeway(leeway),
			),
		}
		v.allow[iss.Name] = map[string]bool{}
	}

	for _, entry := range cfg.Allow {
		name, subject, _ := splitAllow(entry)
		v.allow[name][subject] = true
	}
	return v, nil
}

// Errors wrap ErrUnauthenticated or ErrForbidden; the detail is for logs, not for the caller.
func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	var unverified jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(token, &unverified); err != nil {
		return Identity{}, fmt.Errorf("%w: malformed token: %v", ErrUnauthenticated, err)
	}
	iss, ok := v.byIss[unverified.Issuer]
	if !ok {
		return Identity{}, fmt.Errorf("%w: untrusted issuer %q", ErrUnauthenticated, unverified.Issuer)
	}

	var claims jwt.RegisteredClaims
	parsed, err := iss.parser.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if kid, _ := t.Header["kid"].(string); kid == "" {
			return nil, errors.New("token has no kid")
		}
		if iss.cfg.Type != "" {
			if typ, _ := t.Header["typ"].(string); !strings.EqualFold(typ, iss.cfg.Type) {
				return nil, fmt.Errorf("token typ %q, want %q", typ, iss.cfg.Type)
			}
		}
		return iss.keys.KeyfuncCtx(ctx)(t)
	})
	if err != nil || !parsed.Valid {
		return Identity{}, fmt.Errorf("%w: issuer %s: %v", ErrUnauthenticated, iss.cfg.Name, err)
	}

	id := Identity{Issuer: iss.cfg.Name, Subject: claims.Subject}
	if id.Subject == "" {
		return id, fmt.Errorf("%w: issuer %s: token has no sub", ErrUnauthenticated, iss.cfg.Name)
	}
	if allowed := v.allow[iss.cfg.Name]; !allowed["*"] && !allowed[id.Subject] {
		return id, fmt.Errorf("%w: %s:%s is not allowed", ErrForbidden, iss.cfg.Name, id.Subject)
	}
	return id, nil
}

func (v *Verifier) ReadinessChecks() map[string]func(context.Context) error {
	checks := map[string]func(context.Context) error{}
	for _, iss := range v.byIss {
		checks["jwks:"+iss.cfg.Name] = func(ctx context.Context) error {
			keys, err := iss.keys.Storage().KeyReadAll(ctx)
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				return errors.New("no keys loaded")
			}
			return nil
		}
	}
	return checks
}
