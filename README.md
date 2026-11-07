# Risk

Risk scores transactions for fraud risk as they post. Risk consumes transactions from Kafka, evaluates each one against its account's recent history with configurable rules, publishes the result for downstream systems, and serves past results over HTTP.

Risk assesses but doesn't decide. What to do about a risky transaction is up to the systems that subscribe to its results.

## Features

- **Rule-based scoring**: 14 rules (velocity, high amounts, unusual times, new payees, devices, countries and creditors, pass-through, high-risk merchants and categories, and more), each configurable as named instances with their own thresholds and scores.
- **Explainable results**: every rule outcome carries a reason code and machine-readable evidence, so downstream systems can say why a transaction was flagged.
- **History-aware**: each transaction is judged against the account's own history (90 days by default), with per-rule control of which transactions form the baseline.
- **Never blocks**: a transaction that can't be evaluated is published as `UNKNOWN` with a failure reason and copied to a dead-letter topic. An outage pauses processing rather than failing transactions.
- **At-least-once and ordered**: each account's transactions are evaluated in order, and redeliveries are detected and skipped.
- **Scales out**: run more instances, up to the number of partitions on the incoming topic.
- **Secure by configuration**: TLS and SASL to Kafka, verified TLS to Postgres, optional HTTPS and JWT authentication on the API.
- **Operable**: JSON logs, Prometheus metrics, health and readiness endpoints, embedded database migrations.

## Scope

Risk does risk analysis only: it scores each transaction and explains the score. It doesn't decide or act on the result.

- **In scope**: scoring, risk bands, reason codes and evidence, and publishing and storing the result.
- **Out of scope**: blocking, holding or reversing transactions, alerting customers, opening cases, and any other action. Downstream subscribers decide what a score means for them and what to do about it. Turning reason codes and evidence into readable language is left to them too, so each can word it for its own audience.

## Prerequisites

- Go 1.27+
- Docker with Compose (local stack, end-to-end and load tests)
- `jq` and `perl` (demo and load test only)

## Quick Start

Run every command from the root of this repository, where the `Makefile` and `compose.yaml` are.

### 1. Start Kafka and Postgres

```bash
make stack/up
```

### 2. Create the schema

```bash
make migrate/up
```

### 3. Run the service

```bash
make run
```

This runs one instance against the local stack on `:4000` with TLS and auth off, using [`config.json`](config.json).

### 4. Send a transaction

```bash
docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
  --bootstrap-server localhost:19092 --topic txs.incoming \
  --reader-property parse.key=true --reader-property key.separator='|' <<'EOF'
3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14|{"id":"0b7d2c1e-6a4f-4e8b-9c3d-2f1a5e7b9d04","accountId":"3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14","transactionType":"card","amount":-95000,"postedAt":"2026-11-09T04:10:00+02:00","mcc":"5732","paymentChannel":"online","merchantName":"Gadget Hub","merchantCountry":"ZA"}
EOF
```

The key before `|` is the account ID, which keeps each account's transactions in order.

### 5. Read the result

```bash
curl -s localhost:4000/v1/evaluated-txs/0b7d2c1e-6a4f-4e8b-9c3d-2f1a5e7b9d04 | jq
```

The same result is published to the `txs.evaluated` topic.

### 6. See some fraud

That transaction is ordinary, so it scores `LOW`. With the service still running, in another terminal:

```bash
make demo
```

This sends a few scenarios designed to trigger rules and shows how each was scored:

```
SCENARIO                                             RISK    SCORE  TRIGGERED
Everyday grocery payment                             LOW     0      -
Money in, then out to a payee added 10 min earlier   HIGH    100    highAmountSpend, freshPayee, payeeNameMismatch, passThrough
First gambling payment, R6,000                       MEDIUM  50     highAmountSpend, highRiskMCC
Represented debit order above its mandate max        HIGH    75     aboveMandateMax, representedDebit, highRiskCategory
Transfer to a crypto exchange                        LOW     20     highRiskCategory
Large card refund                                    LOW     25     highAmountRefund
Own-account transfer from a new device               LOW     25     newDevice
```

## Configuration

Behaviour comes from a JSON config file, while connection settings and secrets come from flags and environment variables.

### Config file

Passed with `-config`. Unknown keys are rejected, and every problem is reported at startup.

```jsonc
{
  "bands": { "minMedium": 30, "minHigh": 60 }, // score → LOW / MEDIUM / HIGH
  "rules": [
    {
      "name": "highAmountSpend", // instance name, shown in results
      "rule": "highAmount", // which rule
      "enabled": true,
      "score": 30, // added when it triggers
      "appliesTo": ["card", "transferOut"], // kinds of tx it judges
      "multiplier": 3,
      "minBaselineTxs": 5,
      "defaultThreshold": 500000,
    },
  ],
  "history": { "lookback": "2160h" }, // 90 days of history per evaluation
  "consumer": {
    "topic": "txs.incoming",
    "group": "risk",
    "deadLetterTopic": "txs.incoming.dlq",
    "retry": {
      "failAfterAttempts": 5,
      "initialBackoff": "1s",
      "maxBackoff": "1m",
    },
    "workers": 16, // accounts evaluated concurrently per instance
    "maxPendingPerPartition": 10000, // how far a partition may run ahead of its slowest account
  },
}
```

Durations are Go duration strings such as `"15m"`, `"24h"` or `"2160h"`, and there is no days unit. Amounts are in minor units (cents). See [Rules](docs/rules.md) for every rule and its parameters.

### Flags and environment

`risk run` takes:

| Flag                                | Env                        | Description                                                             | Default      |
| ----------------------------------- | -------------------------- | ----------------------------------------------------------------------- | ------------ |
| `-config`                           |                            | Path to the config file                                                 | required     |
| `-http-addr`                        |                            | HTTP listen address                                                     | `:4000`      |
| `-log-level`                        |                            | `debug`, `info`, `warn` or `error`                                      | `info`       |
| `-db-dsn`                           | `RISK_DB_DSN`              | Postgres URL whose `sslmode` is `verify-full`, `verify-ca` or `disable` | required     |
| `-db-max-open-conns`                |                            | Max open connections                                                    | `25`         |
| `-db-max-idle-conns`                |                            | Max idle connections                                                    | `25`         |
| `-db-max-idle-time`                 |                            | Max connection idle time                                                | `15m`        |
| `-kafka-brokers`                    | `RISK_KAFKA_BROKERS`       | Seed brokers, comma separated                                           | required     |
| `-kafka-tls`                        |                            | `on` or `off`                                                           | required     |
| `-kafka-tls-ca`                     |                            | CA for the brokers' certificates                                        | system roots |
| `-kafka-tls-cert`, `-kafka-tls-key` |                            | Client certificate for mTLS                                             |              |
| `-kafka-sasl`                       |                            | `none`, `plain`, `scram-sha-256` or `scram-sha-512`                     | `none`       |
| `-kafka-sasl-user`                  |                            | SASL user (the API key on Confluent Cloud)                              |              |
| `-kafka-sasl-password`              | `RISK_KAFKA_SASL_PASSWORD` | SASL password                                                           |              |
| `-http-tls`                         |                            | `on` or `off`                                                           | required     |
| `-http-tls-cert`, `-http-tls-key`   |                            | Server certificate and key, reloaded when the files change              |              |
| `-auth`                             |                            | Bearer-token auth on the API: `on` or `off`                             | required     |
| `-auth-issuer`                      |                            | Trusted token issuer, repeatable                                        |              |
| `-auth-allow`                       |                            | Allowed caller, repeatable: `issuer:subject` or `issuer:*`              |              |

Flags win over environment variables. Pass secrets through the environment so they stay out of process listings.

**Important:** `-kafka-tls`, `-http-tls` and `-auth` have no default on purpose, so every deployment states whether it encrypts and authenticates. See [Security](docs/security.md) before turning them off.

## Endpoints

| Endpoint                       | Auth    | Description                                                                      |
| ------------------------------ | ------- | -------------------------------------------------------------------------------- |
| `GET /v1/healthcheck`          | no      | Process is up: `{"status":"available","version":"1.0.0"}`                        |
| `GET /v1/readycheck`           | no      | Postgres, Kafka and token key sets reachable, or `503` with per-check status     |
| `GET /metrics`                 | no      | Prometheus metrics                                                               |
| `GET /v1/evaluated-txs`        | `-auth` | List evaluated transactions, newest first                                        |
| `GET /v1/evaluated-txs/{txID}` | `-auth` | One evaluated transaction                                                        |

List parameters:

| Parameter    | Description                                                           |
| ------------ | --------------------------------------------------------------------- |
| `account_id` | Only this account                                                     |
| `from`, `to` | Posted at or after `from`, before `to` (RFC 3339)                     |
| `risk`       | `UNKNOWN`, `LOW`, `MEDIUM` or `HIGH`                                  |
| `page_size`  | 1–100, default 50                                                     |
| `cursor`     | Pass the previous page's `nextCursor`. It is `null` on the last page. |

```bash
curl -s 'localhost:4000/v1/evaluated-txs?account_id=3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14&risk=HIGH&page_size=20' | jq
```

Errors are `{"error": ...}`, and a bad query returns `422` with a message per parameter.

## Migrations

Migrations are embedded in the binary and applied with the `migrate` command, never on service startup.

```bash
risk migrate up                   # apply pending migrations
risk migrate version              # applied version, and whether one was left half-done
risk migrate down -steps 1        # roll back one migration
risk migrate force -version 2     # clear a half-done flag after repairing by hand
```

It takes the same `-db-dsn` / `RISK_DB_DSN` as `run`. Run it as its own deployment step, as described in [Operations](docs/operations.md#migrations).

Account profiles (hourly activity and the payees, devices, countries, MCCs and creditors each account has used) are kept up to date as transactions are stored. `risk profiles rebuild` recomputes them from stored transactions, and [Operations](docs/operations.md#rebuilding-profiles) explains when to run it.

**Important:** a migration using `create index concurrently` must be the only statement in its file, because Postgres won't run it inside the implicit transaction a multi-statement file gets.

## Load Testing

Seeds history, queues transactions, and times how fast the instances work through them:

```bash
make loadtest ARGS="-n 20000 -accounts 2000 -history 50 -instances 3 -partitions 6"
```

The report shows throughput, per-instance split, stage latencies (p50/p95/p99), connection-pool waits, the risk mix and table size.

To check that busy accounts don't slow down the rest, add a few accounts with long histories and send them a share of the traffic:

```bash
make loadtest ARGS="-n 10000 -accounts 1000 -history 100 -hot-accounts 5 -hot-history 10000 -hot-share 0.1"
```

The report then also shows when ordinary and hot transactions finished (p50, p95 and last), measured from the first one stored. Numbers from a laptop show trends and bottlenecks, not production capacity.

## Development

### Quick Commands (Makefile)

| Command                            | Description                                                 |
| ---------------------------------- | ----------------------------------------------------------- |
| `make build`                       | Build `bin/risk`                                            |
| `make image`                       | Build the container image, tagged `risk:<commit>`           |
| `make test`                        | Unit tests with the race detector                           |
| `make audit`                       | Formatting, vet, tidy and verified modules (what CI checks) |
| `make e2e`                         | End-to-end tests: three instances against the local stack   |
| `make demo`                        | Send scenarios that trigger rules to a running instance     |
| `make loadtest`                    | Load test                                                   |
| `make golden`                      | Regenerate golden files after an intended format change     |
| `make stack/up`, `make stack/down` | Start or remove the local stack                             |
| `make help`                        | Everything else                                             |

**Important:** `make stack/down` deletes the local stack's data.

### Project Structure

```
cmd/risk/          CLI (run, migrate, version), HTTP API, flags, TLS
config/            config file loading and validation
risk/              domain types: transactions, kinds, outcomes, reason codes
risk/engine/       rule scoping and scoring, with one file per rule in rules/
risk/consumer/     validates incoming messages, decides how failures are reported
risk/ops/          evaluate a transaction: history, rules, publish, store
risk/contract/     wire formats for incoming and published messages
auth/              JWT verification and key sets
kafka/             consumer, per-account dispatch, settler (retries, fail-through, dead-letter), publisher
db/                Postgres access and storage format
metrics/           Prometheus metrics
migrations/        SQL migrations, embedded in the binary
e2e/               end-to-end tests
demo/              demo script
loadtest/          load test script
compose.yaml       local stack: Kafka and Postgres
```

### Running Tests

- **Unit**: `make test`. No external services.
- **End-to-end**: `make e2e`. Starts the stack if needed and leaves it running for reruns. Covers rebalances, crashes and a Postgres outage.
- **Golden files**: message and storage formats are pinned in `risk/contract/testdata` and `db/testdata`. A failing golden test means the wire or storage format changed. If the change was intended, run `make golden` and review the diff. A storage change also needs a migration.

## Architecture

```
  producer
     │  key = accountId
     ▼
  txs.incoming ───────────────▶ risk instances (one consumer group)
                                     │
                                     ▼
                           workers (by account)
                                     │
                                     ▼
                                  settler ──── can't evaluate ───▶ txs.incoming.dlq
                                     │                             + UNKNOWN to txs.evaluated
                                     ▼
                              decode + validate
                                     │
                                     ▼
                                load history ◀──────────┐
                                     │                  │
                                     ▼                  │
                                 score rules            │
                                     │                  │
                                     ▼                  │
                       publish ──▶ txs.evaluated        │
                                     │                  │
                                     ▼                  │
                                insert ──────▶ Postgres ┘
                                                  ▲
                    HTTP API (TLS, JWT) ── reads ─┘
```

1. Each instance owns some partitions of `txs.incoming`. Within an instance, each account always goes to the same one of `workers` workers, so its transactions are evaluated in order while other accounts proceed in parallel. A busy account holds up only the accounts sharing its worker. If a partition gets `maxPendingPerPartition` records ahead of its slowest account, fetching from that partition pauses until it catches up.
2. The settler runs each record through the pipeline, retrying transient errors with backoff.
3. The pipeline loads the account's history, scores the transaction, publishes the result keyed by account, then stores it.
4. A record that can't be evaluated is copied to the dead-letter topic and published as `UNKNOWN` with a failure reason, and processing moves on.
5. While Postgres or Kafka is down, nothing fails through: the settler waits and retries.

## Documentation

| Guide                                | For                                                           |
| ------------------------------------ | ------------------------------------------------------------- |
| [Rules](docs/rules.md)               | How scoring works and every rule's parameters                 |
| [Security](docs/security.md)         | Postgres, Kafka, HTTPS and JWT authentication                 |
| [Operations](docs/operations.md)     | Deploying, monitoring, failures and dead-letter replay        |
| [Message Contract](docs/contract.md) | Producer and downstream teams: message formats and guarantees |
