# Operations

Deploying, monitoring and recovering Risk.

## Deploying

### Migrations

Apply migrations as their own step before rolling out a new version, never from the service on startup:

```bash
risk migrate up
```

- Run it as a Kubernetes Job, a Helm `pre-install,pre-upgrade` hook or an init container, using the same image as the service.
- Give it a database user that can create and alter tables. The service's user only needs to read and write rows.
- Concurrent runs are safe: migrations take a Postgres advisory lock.
- `risk migrate version` reports `"dirty": true` if a migration failed partway. Fix the database by hand, then `risk migrate force -version <last good>` and run `up` again.

**Important:** migrations must stay compatible with the running version, because old instances keep running during a rollout. Add columns and indexes first, and remove them in a later release.

### Topics

Create these before starting Risk, because it doesn't create topics.

| Topic | Key | Partitions | Retention |
|---|---|---|---|
| `txs.incoming` | account ID | Plan for peak load, as described in [Scaling](#scaling) | Longer than the longest outage you'd want to recover from |
| `txs.incoming.dlq` | account ID | 1 is enough | Long, e.g. 30 days, so failures can be replayed after a fix |
| `txs.evaluated` | account ID | Same as `txs.incoming` | Whatever downstream needs |

Names are set in the config file (`consumer.topic`, `consumer.deadLetterTopic`), while `txs.evaluated` is fixed.

### Probes

| Probe | Endpoint | Meaning |
|---|---|---|
| Liveness | `GET /v1/healthcheck` | The process is up |
| Readiness | `GET /v1/readycheck` | Postgres, Kafka and every token issuer's keys are reachable |

```yaml
livenessProbe:
  httpGet: { path: /v1/healthcheck, port: 4000, scheme: HTTPS }   # HTTP with -http-tls off
readinessProbe:
  httpGet: { path: /v1/readycheck, port: 4000, scheme: HTTPS }
  periodSeconds: 10
```

**Important:** don't use readiness as liveness. When Postgres is down every instance becomes unready, and restarting them wouldn't help and would lose their place.

### Retention

Risk doesn't delete rows. Remove old rows outside the service, with partitioning (pg_partman) or a scheduled job:

| Table | Holds | Delete rows where |
|---|---|---|
| `evaluated_txs` | Every evaluated transaction, and the source of truth for profiles | `posted_at` is older than your retention period |
| `account_hours` | Per account, kind and hour: count, money in and out | `hour` is older than `history.lookback` |
| `account_seen` | Per account: payees, devices, countries, MCCs and creditors used | `last_seen` is older than `history.lookback` |

**Important:** keep `evaluated_txs` for longer than `history.lookback` plus a margin. Profiles can only be rebuilt from the transactions still there, and a rebuild after deleting rows inside the lookback silently shortens every account's history and changes scores.

### Rebuilding Profiles

Each account's profile is updated in the same statement that stores its transaction, so it never needs rebuilding in normal running. Rebuild it from `evaluated_txs` when:

- `history.lookback` grows, so profiles cover the longer period
- a code change alters which values profiles record
- profile tables were lost or edited by hand

```bash
risk profiles rebuild
```

It takes the same `-db-dsn` / `RISK_DB_DSN` as `migrate`, and runs in one transaction: evaluations wait until it commits, so run it in a quiet period or with the service scaled to zero.

### Sizing

- **Database connections:** each instance uses at most `-db-max-open-conns` (default 25). Keep it above `consumer.workers` (default 16), since each worker holds a connection while it evaluates, with a few spare for the HTTP API. Make sure instances × that number fits the database's limit.
- **Throughput:** each instance evaluates up to `consumer.workers` accounts at once, each account's transactions in order. Throughput is then limited by the database more than by the instance, so measure on your own infrastructure with the [load test](../README.md#load-testing).
- **Busy accounts:** an account's transactions are evaluated one after another, so a burst on one account holds up only the accounts that share its worker (1 in `workers`). More workers shrink that share, at the cost of more concurrent database work.
- **How far ahead a partition may run:** a partition is committed only up to its oldest unsettled record, so records settled after a slow account's record stay pending until it finishes. `consumer.maxPendingPerPartition` (default 10,000) caps that. At the cap, fetching for the whole partition pauses until the slow account catches up. Raise it to let other accounts run further ahead during a busy account's burst. Each pending record costs about 1 KB of memory.
- **History:** every evaluation reads the account's profile. That covers its hourly summaries and the values it has used within `history.lookback`, its last `medianOfLast` amounts and its raw transactions within the longest rule window. The cost grows with the hours an account was active and the distinct values it used, not with every transaction it made, so busy accounts cost more but not proportionally more.

## Shutdown and Scaling

On `SIGTERM`, Risk stops taking new transactions, cancels those in progress, drops those still queued for a worker, commits each partition up to its oldest unsettled transaction, leaves the consumer group, and stops the HTTP server within 5 seconds. Transactions after that point are evaluated again by the partition's next owner, which skips any already stored.

### Scaling

- Add instances to process more. Each owns some partitions of `txs.incoming`.
- Instances beyond the number of partitions sit idle.
- Choose the partition count up front. Adding partitions later moves accounts to different partitions, and during the move an account's transactions can be evaluated out of order.

## Monitoring

All metrics are on `GET /metrics`.

| Metric | Labels | Description |
|---|---|---|
| `risk_txs_evaluated_total` | `risk` | Transactions evaluated and published, by risk |
| `risk_rule_triggered_total` | `rule` | Rule outcomes that triggered, by rule instance |
| `risk_evaluation_stage_seconds` | `stage` | Time per stage: `load_history` (the account's profile), `evaluate`, `publish`, `insert` |
| `risk_txs_skipped_total` | `reason` | Redelivered transactions skipped because they were already evaluated |
| `risk_settle_failures_total` | `kind` | Failed attempts: `transient`, `unexpected`, `unprocessable` |
| `risk_txs_failed_through_total` | | Transactions published as `UNKNOWN` and dead-lettered |
| `risk_txs_dead_lettered_total` | | Records with no transaction or account ID, dead-lettered only |
| `go_sql_*` | | Connection pool: in use, idle, waits |
| `go_*`, `process_*` | | Go runtime and process |

### Suggested Alerts

| Alert | Condition | Likely cause |
|---|---|---|
| Transactions failing through | `rate(risk_txs_failed_through_total[5m]) > 0` | Bad data from the producer, or a bug. Check the `failure-reason` on the dead-letter topic |
| Stuck on retries | `rate(risk_settle_failures_total{kind="transient"}[5m])` high for several minutes | Postgres or Kafka unavailable |
| Falling behind | Consumer lag on `txs.incoming` growing (from your Kafka monitoring) | Too few instances or partitions, slow database |
| Slow evaluations | p99 of `risk_evaluation_stage_seconds` above your target | Large histories, database load |
| Pool exhausted | `rate(go_sql_wait_count_total[5m]) > 0` | Too few connections for the load |
| Not ready | Readiness failing on all instances | A dependency is down |

Risk doesn't export consumer lag itself, so take it from the broker or a Kafka exporter.

## When Things Fail

| Situation | What Risk does |
|---|---|
| Postgres or Kafka unavailable | Retries with backoff (`consumer.retry`) until it's back. Nothing is published as `UNKNOWN`, and readiness fails |
| Invalid message (bad JSON, missing or malformed fields) | Dead-letters it and publishes `UNKNOWN` with `failureReason: INVALID` |
| Same transaction ID with different content | Keeps the stored version, dead-letters the new one, publishes `UNKNOWN` with `CONFLICT` |
| Unexpected error, repeated `failAfterAttempts` times while dependencies are healthy | Dead-letters it and publishes `UNKNOWN` with `INTERNAL` |
| No transaction or account ID | Dead-letters it only, because there is nothing to publish under |
| Same message delivered again | Skips it |
| Instance crashes | Another instance re-reads uncommitted messages and skips those already evaluated |

Transactions published as `UNKNOWN` are not stored, so they don't count in later transactions' history.

## Replaying the Dead-Letter Topic

Each dead-letter copy keeps the original key, value and headers, plus:

| Header | Value |
|---|---|
| `failure-reason` | `INVALID`, `CONFLICT` or `INTERNAL` |
| `error` | The full error, for diagnosis |

Only `INTERNAL` failures are worth replaying, after the cause is fixed. `INVALID` ones fail again, so the producer must resend corrected data. `CONFLICT` ones need someone to decide which version is right.

### 1. Fix and deploy the cause

Replaying before the fix sends the same transactions straight back to the dead-letter topic.

### 2. Export the dead-letter topic

```bash
kafka-console-consumer.sh --bootstrap-server $BROKERS --command-config client.properties \
  --topic txs.incoming.dlq --from-beginning --timeout-ms 15000 \
  --formatter-property print.offset=true --formatter-property print.headers=true \
  --formatter-property print.key=true --formatter-property headers.separator=';' \
  --formatter-property key.separator=$'\t' > dlq.tsv
```

`client.properties` holds the same TLS and SASL settings as the service. Each line is `Offset:N`, headers, key and value, separated by tabs.

### 3. Pick the records to replay

```bash
grep -P '(^|;)failure-reason:INTERNAL(;|\t)' dlq.tsv \
  | awk -F'\t' '{ print $3 "|" $4 }' > replay.txt
wc -l replay.txt && head replay.txt
```

Narrow it further by offset, account or transaction ID as needed. Note the highest offset you replay, so the next replay can start after it.

### 4. Send them back

```bash
kafka-console-producer.sh --bootstrap-server $BROKERS --command-config client.properties \
  --topic txs.incoming --reader-property parse.key=true --reader-property key.separator='|' < replay.txt
```

### 5. Check

- `GET /v1/evaluated-txs/{txID}` returns the transaction with a real risk.
- `risk_txs_evaluated_total` went up and `risk_txs_failed_through_total` didn't.

Replaying a record twice is harmless: already evaluated transactions are skipped. Downstream receives a second message for each replayed transaction, which replaces the earlier `UNKNOWN`. Original headers other than the key are not resent.

## Logs

Logs are JSON on stdout, one event per line, timestamps in UTC. Every line has `time`, `level`, `msg`, `app` and `version`. Risk doesn't write or ship log files. Collecting stdout into a log store is left to the platform's log agent.

`risk run` logs:

| Message | Level | Fields |
|---|---|---|
| `starting` | info | listen address, TLS and auth settings, topic, group |
| `partitions assigned` | info | `partitions` (topic to partition numbers) |
| `partitions revoked` | info | `partitions` |
| `partitions lost` | warn | `partitions` |
| `tx evaluated` | info | `txID`, `accountID`, `score`, `risk` |
| `tx already evaluated, skipping` | info | `txID`, `accountID` |
| `tx not settled, retrying` | warn | `partition`, `offset`, `failures`, `err` |
| `tx published as UNKNOWN` | error | `accountID`, `partition`, `offset`, `cause` |
| `tx dead-lettered` | error | `accountID` (may be empty), `partition`, `offset`, `cause` |
| `fetch failed` | error | `topic`, `partition`, `err` |
| `commit on revoke failed` | error | `err` |
| `request rejected` | warn | `err` (reason, issuer, subject), `method`, `path` |
| `request failed` | error | `err`, `method`, `path` |
| `readiness check failed` | warn | `check`, `err` |
| `jwks refresh failed` | warn | `issuer`, `err` |
| `tls certificate reload failed, keeping the previous one` | warn | `err` |
| `http server stopped` | error | `err` |
| `http shutdown failed` | error | `err` |
| `shutting down` | info | |
| `stopped` | info | Clean exit, after the consumer has left the group and the HTTP server has stopped |
| `exiting` | error | `err`: startup or run failed |

`risk migrate` and `risk profiles rebuild` log:

| Message | Level | Fields |
|---|---|---|
| `migrations` | info | `action`, `schemaVersion`, `dirty` |
| each migration applied, e.g. `1/u create_evaluated_txs_table` | info | |
| `migrate failed` | error | `action`, `err` |
| `profiles rebuilt` | info | `hourRows`, `seenRows` |
| `profiles rebuild failed` | error | `err` |

At `info`, every transaction logs one line. Use `-log-level warn` to keep only problems.
