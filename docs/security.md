# Security

How Risk connects to Postgres and Kafka, and how its HTTP API is encrypted and authenticated.

## Postgres

The DSN must be a `postgres://` URL with `sslmode` set to one of:

| `sslmode` | Use |
|---|---|
| `verify-full` | Production: encrypts and checks the server's certificate and host name |
| `verify-ca` | Encrypts and checks the certificate, not the host name |
| `disable` | Local development only |

`require` and `prefer` are rejected: they encrypt without checking who the server is, so they can't detect impersonation.

```bash
# Amazon RDS, with the RDS CA bundle mounted in the container
export RISK_DB_DSN='postgres://risk:<password>@mydb.xxxx.eu-west-1.rds.amazonaws.com:5432/risk?sslmode=verify-full&sslrootcert=/certs/rds-global-bundle.pem'
```

Pass the DSN through `RISK_DB_DSN`, not `-db-dsn`, so the password stays out of process listings. Risk never logs the DSN.

**Important:** use separate database users for migrations and for the service. The service only reads and writes rows, and only the migration step needs to create and alter tables.

## Kafka

`-kafka-tls on|off` is required. SASL is only allowed with TLS on, so credentials never cross the network unencrypted.

| Setup | Flags |
|---|---|
| Self-hosted, mutual TLS | `-kafka-tls on -kafka-tls-ca /certs/ca.crt -kafka-tls-cert /certs/client.crt -kafka-tls-key /certs/client.key` |
| Confluent Cloud | `-kafka-tls on -kafka-sasl plain -kafka-sasl-user <API key>`, with `RISK_KAFKA_SASL_PASSWORD=<API secret>` |
| Amazon MSK (SASL/SCRAM) | `-kafka-tls on -kafka-sasl scram-sha-512 -kafka-sasl-user <user>`, with `RISK_KAFKA_SASL_PASSWORD` |
| Redpanda | Either of the above, depending on how the cluster is configured |
| Local stack | `-kafka-tls off` |

Without `-kafka-tls-ca`, broker certificates are checked against the system roots, which is right for Confluent Cloud and MSK. Every broker's certificate is checked against its own host name.

## HTTPS

`-http-tls on|off` is required.

- **`on`**: the API port serves HTTPS (TLS 1.2+, HTTP/2) using `-http-tls-cert` and `-http-tls-key`.
- **`off`**: plain HTTP, for when a service mesh or gateway in front terminates TLS.

The certificate is reloaded when its files change, so renewals by cert-manager or a rotated Kubernetes secret need no restart. A renewal that fails to load is logged and the previous certificate keeps serving.

Health, readiness and metrics stay on the same port. Configure Kubernetes probes with `scheme: HTTPS` and Prometheus with `scheme: https` and the CA.

## Authentication

`-auth on|off` is required. With `on`, the evaluated-transaction endpoints require a bearer token (JWT). Health, readiness and metrics stay open for probes and scrapers.

### How It Works

1. The caller sends `Authorization: Bearer <token>`.
2. Risk reads the token's `iss` and picks the matching configured issuer. Tokens from any other issuer are rejected.
3. The signature is checked with that issuer's public keys only, using an allowed algorithm (`RS256` and `ES256` by default). Unsigned (`none`) and shared-secret (`HS*`) tokens are always rejected.
4. `aud` must contain the issuer's configured audience, `exp` is required and checked, and `nbf` is checked, with 30 seconds' tolerance for clock skew.
5. The token's `sub` must be on the allowlist for that issuer.

```
caller                         risk                               issuer
  │  GET /v1/evaluated-txs        │                                  │
  │  Authorization: Bearer …  ──▶ │                                  │
  │                               │  key set (cached, refreshed      │
  │                               │  every 15m) ◀──────────────────── │
  │                               │  verify iss, signature, aud,     │
  │                               │  exp, nbf; sub on allowlist      │
  │  ◀── 200 / 401 / 403 ──────── │                                  │
```

Keys are fetched from the issuer's key set (JWKS) at startup and every 15 minutes. A token signed with a key Risk hasn't seen triggers a refresh, at most once a minute, so key rotation works without a restart. If the issuer is unreachable, the last keys fetched stay in use.

### Configuring Issuers

`-auth-issuer` takes comma-separated `key=value` pairs and can be repeated, one per issuer:

| Key | Description | Required |
|---|---|---|
| `name` | Short name used in `-auth-allow` and logs | yes |
| `iss` | Exact `iss` value in the issuer's tokens | yes |
| `jwks` | HTTPS URL of the issuer's key set | yes |
| `aud` | Audience Risk's tokens must carry | yes |
| `algs` | Allowed algorithms, `\|`-separated | no, `RS256\|ES256` |
| `typ` | Required token type header, e.g. `at+jwt` | no |
| `ca` | CA file for the key-set URL | no, system roots |
| `token` | File with a bearer token for fetching the key set, re-read on every fetch | no |

`-auth-allow` is repeatable: `issuer:subject` allows one caller, `issuer:*` allows any caller that issuer vouches for. Every issuer needs at least one entry.

### Kubernetes Service Accounts

Services in the same cluster can authenticate with tokens Kubernetes issues to their pods, with no client secrets.

**1. The calling service mounts a token for Risk's audience:**

```yaml
spec:
  serviceAccountName: case-manager        # namespace: payments
  containers:
    - name: app
      volumeMounts:
        - { name: risk-token, mountPath: /var/run/secrets/risk, readOnly: true }
  volumes:
    - name: risk-token
      projected:
        sources:
          - serviceAccountToken:
              audience: risk
              expirationSeconds: 600
              path: token
```

Kubernetes refreshes the file before it expires, and the caller re-reads it and sends it as the bearer token.

**2. Find the cluster's issuer and key-set URL:**

```bash
kubectl get --raw /.well-known/openid-configuration | jq '{issuer, jwks_uri}'
```

**3. Configure Risk:**

```bash
-auth on
-auth-issuer name=cluster,iss=https://kubernetes.default.svc.cluster.local,jwks=https://kubernetes.default.svc/openid/v1/jwks,aud=risk,ca=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt,token=/var/run/secrets/kubernetes.io/serviceaccount/token
-auth-allow cluster:system:serviceaccount:payments:case-manager
```

On EKS and GKE the issuer and key set are public URLs, so `ca` and `token` aren't needed.

**Important:** any pod in the cluster can get a token with audience `risk` for its own service account. The allowlist is what limits access to the services you name.

**Important:** tokens are checked locally, so a token stays valid until it expires even after its pod is deleted. Keep `expirationSeconds` short (600 is the minimum Kubernetes allows).

### Identity Providers

Services outside the cluster can use client-credentials tokens from an identity provider such as Keycloak, Okta or Entra ID:

```bash
-auth-issuer name=idp,iss=https://idp.example.com/realms/prod,jwks=https://idp.example.com/realms/prod/protocol/openid-connect/certs,aud=risk-api,typ=at+jwt
-auth-allow idp:fraud-ui
```

Use `idp:*` only if the identity provider itself decides which clients may get a token for Risk's audience.

Several issuers can be trusted at once. A subject is always tied to its issuer, so a service account in one cluster can't stand in for one elsewhere.

### Responses

| Status | When |
|---|---|
| `401` with `WWW-Authenticate: Bearer realm="risk"` | No bearer token |
| `401` with `error="invalid_token"` | Token fails any check |
| `403` | Valid token, caller not on the allowlist |

Responses don't say which check failed, but the reason, issuer and subject are logged at `warn`. Tokens are never logged.

## Choosing a Setup

| `-auth` | `-http-tls` | When |
|---|---|---|
| `off` | `off` | A service mesh provides mutual TLS and decides who may call Risk |
| `on` | `off` | A mesh or gateway encrypts traffic, Risk checks tokens |
| `on` | `on` | No mesh: Risk encrypts and checks tokens |
| `off` | `on` | Encrypted, but anyone who can reach the port can call it |

**Important:** don't run `-auth on` with `-http-tls off` unless something in front encrypts the traffic, because bearer tokens sent in plain text can be copied and reused until they expire.

Whichever you choose, limit network access (for example with a Kubernetes NetworkPolicy) so the API is only reachable through the mesh, gateway or the callers you expect.

## Logs and Personal Data

- Never logged: database DSNs and passwords, Kafka passwords, bearer tokens, counterparty names, references and proxies.
- Logged: transaction and account IDs, which are pseudonymous identifiers, plus failure causes.
- Published to downstream and returned by the API: the full transaction, including personal fields. Limit who can read `txs.evaluated` and call the API accordingly.
