package auth

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

var DefaultAlgorithms = []string{"RS256", "ES256"}

var asymmetricAlgorithms = []string{
	"RS256", "RS384", "RS512",
	"PS256", "PS384", "PS512",
	"ES256", "ES384", "ES512",
	"EdDSA",
}

type Issuer struct {
	Name       string
	Issuer     string
	JWKSURL    string
	Audience   string
	Algorithms []string
	Type       string
	CAFile     string
	TokenFile  string
}

// Allow entries are "issuerName:subject", or "issuerName:*" to accept any subject the issuer vouches for.
type Config struct {
	Issuers []Issuer
	Allow   []string
}

func (c Config) Validate() error {
	var errs []error
	require := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	require(len(c.Issuers) > 0, "auth needs at least one issuer")
	names, isses := map[string]bool{}, map[string]bool{}
	for _, iss := range c.Issuers {
		require(iss.Name != "" && !strings.Contains(iss.Name, ":"), "auth issuer name %q must be set and contain no colon", iss.Name)
		require(!names[iss.Name], "auth issuer name %q is repeated", iss.Name)
		require(iss.Issuer != "", "auth issuer %s: iss must be set", iss.Name)
		require(!isses[iss.Issuer], "auth issuer %s: iss %q is used by another issuer", iss.Name, iss.Issuer)
		u, err := url.Parse(iss.JWKSURL)
		require(err == nil && u.Scheme == "https" && u.Host != "", "auth issuer %s: jwks must be an https URL", iss.Name)
		require(iss.Audience != "", "auth issuer %s: aud must be set", iss.Name)
		require(len(iss.Algorithms) > 0, "auth issuer %s: algs must list at least one algorithm", iss.Name)
		for _, alg := range iss.Algorithms {
			require(slices.Contains(asymmetricAlgorithms, alg), "auth issuer %s: alg %q not allowed; use one of %v", iss.Name, alg, asymmetricAlgorithms)
		}
		names[iss.Name], isses[iss.Issuer] = true, true
	}

	allowed := map[string]bool{}
	for _, entry := range c.Allow {
		name, subject, ok := splitAllow(entry)
		require(ok && subject != "", "auth allow %q must be issuerName:subject or issuerName:*", entry)
		require(!ok || names[name], "auth allow %q names an unknown issuer", entry)
		allowed[name] = true
	}
	for name := range names {
		require(allowed[name], "auth issuer %s has no allow entry; add %s:<subject> or %s:*", name, name, name)
	}

	return errors.Join(errs...)
}

func splitAllow(entry string) (name, subject string, ok bool) {
	return strings.Cut(entry, ":")
}
