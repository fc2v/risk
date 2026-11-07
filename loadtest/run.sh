#!/usr/bin/env bash
# Backlog load test: seeds history, queues n card txs, and times how fast the instances drain them.
# Needs the local stack: make stack/up
set -euo pipefail

N=20000
ACCOUNTS=2000
HISTORY=100
INSTANCES=6
PARTITIONS=6
TIMEOUT=600
HOT_ACCOUNTS=0
HOT_HISTORY=10000
HOT_SHARE=0.1

usage() {
  echo "usage: $0 [-n txs] [-accounts n] [-history per-account] [-instances n] [-partitions n] [-timeout seconds]"
  echo "       [-hot-accounts n] [-hot-history per-account] [-hot-share fraction-of-txs]"
  exit 2
}
while [ $# -gt 0 ]; do
  case "$1" in
    -n) N=$2; shift 2 ;;
    -accounts) ACCOUNTS=$2; shift 2 ;;
    -history) HISTORY=$2; shift 2 ;;
    -instances) INSTANCES=$2; shift 2 ;;
    -partitions) PARTITIONS=$2; shift 2 ;;
    -timeout) TIMEOUT=$2; shift 2 ;;
    -hot-accounts) HOT_ACCOUNTS=$2; shift 2 ;;
    -hot-history) HOT_HISTORY=$2; shift 2 ;;
    -hot-share) HOT_SHARE=$2; shift 2 ;;
    *) usage ;;
  esac
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
COMPOSE="docker compose -f $ROOT/compose.yaml"
BROKER=localhost:19092
RUN=lt$(date +%s)
DB=risk_$RUN
TOPIC=txs.incoming.$RUN
TMP=$(mktemp -d)
PIDS=""
PORTS=""

log() { printf '%s  %s\n' "$(date +%H:%M:%S)" "$*"; }
now() { perl -MTime::HiRes=time -e 'printf "%.3f", time'; }
psql_q() { $COMPOSE exec -T postgres psql -U postgres -v ON_ERROR_STOP=1 -qtA "$@"; }
kafka() { local tool=$1; shift; $COMPOSE exec -T kafka "/opt/kafka/bin/$tool" --bootstrap-server "$BROKER" "$@"; }

cleanup() {
  status=$?
  for pid in $PIDS; do kill "$pid" 2>/dev/null || true; done
  for pid in $PIDS; do wait "$pid" 2>/dev/null || true; done
  if [ "$status" -ne 0 ]; then
    for f in "$TMP"/risk-*.log; do [ -f "$f" ] && { echo "--- $f"; tail -5 "$f"; }; done
  fi
  psql_q -d postgres -c "drop database if exists $DB with (force)" >/dev/null 2>&1 || true
  kafka kafka-topics.sh --delete --topic "$TOPIC" >/dev/null 2>&1 || true
  kafka kafka-topics.sh --delete --topic "$TOPIC.dlq" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

metrics() { for p in $PORTS; do curl -fs "http://127.0.0.1:$p/metrics"; done; }
counter() { metrics | awk -v re="^$1" '$1 ~ re { s += $2 } END { printf "%d", s }'; }

$COMPOSE ps --status running --services | grep -qx kafka &&
  $COMPOSE ps --status running --services | grep -qx postgres ||
  { echo "local stack not running: make stack/up"; exit 1; }

(cd "$ROOT" && go build -o "$TMP/risk" ./cmd/risk)
export RISK_DB_DSN="postgres://postgres:postgres@localhost:55432/$DB?sslmode=disable"

log "database $DB"
psql_q -d postgres -c "create database $DB"
"$TMP/risk" migrate up -log-level warn

log "seeding $ACCOUNTS accounts x $HISTORY past txs, $HOT_ACCOUNTS hot accounts x $HOT_HISTORY"
psql_q -d "$DB" -v accounts="$ACCOUNTS" -v history="$HISTORY" -v hot="$HOT_ACCOUNTS" -v hothistory="$HOT_HISTORY" <<'SQL'
insert into evaluated_txs (id, account_id, transaction_type, kind, amount, posted_at, category, details, score, risk)
select id, account_id, 'card', 'card', amount, posted_at, 'groceries', details, 0, 'LOW'
from (
  select id, account_id,
         -(500 + (random() * 30000)::int) as amount,
         now() - interval '1 hour' - random() * interval '89 days' as posted_at,
         jsonb_build_object(
           'mcc', (array['5411', '5812', '5732', '5541'])[1 + floor(random() * 4)::int],
           'paymentChannel', 'in store',
           'merchantName', 'Shop ' || floor(random() * 50)::int,
           'merchantCountry', 'ZA') as details
  from (
    select 'seed-' || a || '-' || h as id, 'acc-' || a as account_id
    from generate_series(0, :accounts - 1) a, generate_series(1, :history) h
    union all
    select 'seed-hot-' || a || '-' || h, 'hot-' || a
    from generate_series(0, :hot - 1) a, generate_series(1, :hothistory) h
  ) ids
) rows
order by posted_at;
SQL
"$TMP/risk" profiles rebuild -log-level warn
psql_q -d "$DB" -c "vacuum analyze"

log "topics: $TOPIC ($PARTITIONS partitions)"
kafka kafka-topics.sh --create --topic "$TOPIC" --partitions "$PARTITIONS" --replication-factor 1 >/dev/null
kafka kafka-topics.sh --create --topic "$TOPIC.dlq" --partitions 1 --replication-factor 1 >/dev/null
kafka kafka-topics.sh --create --if-not-exists --topic txs.evaluated --partitions "$PARTITIONS" --replication-factor 1 >/dev/null

jq --arg t "$TOPIC" --arg g "risk-$RUN" \
  '.consumer.topic = $t | .consumer.group = $g | .consumer.deadLetterTopic = ($t + ".dlq")' \
  "$ROOT/config.json" > "$TMP/config.json"

log "starting $INSTANCES instance(s)"
for i in $(seq 1 "$INSTANCES"); do
  port=$((4100 + i))
  "$TMP/risk" run \
    -config "$TMP/config.json" -http-addr "127.0.0.1:$port" \
    -kafka-brokers "$BROKER" -kafka-tls off -auth off -http-tls off -log-level warn \
    > "$TMP/risk-$i.log" 2>&1 &
  PIDS="$PIDS $!"
  PORTS="$PORTS $port"
done
for p in $PORTS; do
  for _ in $(seq 1 60); do curl -fs "http://127.0.0.1:$p/v1/readycheck" >/dev/null && break; sleep 0.5; done
  curl -fs "http://127.0.0.1:$p/v1/readycheck" >/dev/null || { echo "instance on :$p not ready"; exit 1; }
done
sleep 3 # let the group settle so partitions are spread before load arrives

perl -MPOSIX=strftime -e '
  my ($n, $accounts, $run, $hot, $share) = @ARGV; my $now = time; my @mcc = qw(5411 5812 5732 5541 7995);
  for my $i (0 .. $n - 1) {
    my $acc = $hot > 0 && rand() < $share ? "hot-" . int(rand $hot) : "acc-" . int(rand $accounts);
    my $at = strftime("%Y-%m-%dT%H:%M:%SZ", gmtime($now - int(rand 7 * 86400)));
    printf qq(%s|{"id":"%s-%d","accountId":"%s","transactionType":"card","amount":%d,"postedAt":"%s","category":"groceries","mcc":"%s","paymentChannel":"in store","merchantName":"Shop","merchantCountry":"ZA"}\n),
      $acc, $run, $i, $acc, -(500 + int(rand 30000)), $at, $mcc[rand @mcc];
  }' "$N" "$ACCOUNTS" "$RUN" "$HOT_ACCOUNTS" "$HOT_SHARE" > "$TMP/txs"

log "producing $N txs"
start=$(now)
kafka kafka-console-producer.sh --topic "$TOPIC" \
  --reader-property parse.key=true --reader-property key.separator='|' < "$TMP/txs"
produced=$(now)

while :; do
  done_count=$(( $(counter 'risk_txs_evaluated_total') + $(counter 'risk_txs_failed_through_total') + $(counter 'risk_txs_skipped_total') ))
  [ "$done_count" -ge "$N" ] && break
  if [ "$(perl -e "print $(now) - $start > $TIMEOUT ? 1 : 0")" = 1 ]; then
    echo "timed out after ${TIMEOUT}s with $done_count/$N settled"; exit 1
  fi
  sleep 0.25
done
end=$(now)

for p in $PORTS; do curl -fs "http://127.0.0.1:$p/metrics" > "$TMP/metrics-$p"; done
stored=$(psql_q -d "$DB" -c "select count(*) from evaluated_txs where id like '$RUN-%'")
sizes=$(psql_q -d "$DB" -c "select pg_size_pretty(pg_total_relation_size('evaluated_txs')) || ' (table ' || pg_size_pretty(pg_relation_size('evaluated_txs')) || ', indexes ' || pg_size_pretty(pg_indexes_size('evaluated_txs')) || ')'")
finished=$(psql_q -d "$DB" -F ' ' -c "
  select grp, round(percentile_cont(0.5) within group (order by s))::int,
         round(percentile_cont(0.95) within group (order by s))::int, round(max(s))::int
  from (select case when account_id like 'hot-%' then 'hot' else 'ordinary' end as grp,
               extract(epoch from created_at - min(created_at) over ()) as s
        from evaluated_txs where id like '$RUN-%') t
  group by grp order by grp desc")

echo
echo "txs $N  instances $INSTANCES  partitions $PARTITIONS  history/account $HISTORY  accounts $ACCOUNTS"
[ "$HOT_ACCOUNTS" -gt 0 ] && echo "hot accounts $HOT_ACCOUNTS x $HOT_HISTORY history, $(perl -e "print $HOT_SHARE * 100")% of txs"
perl -e 'printf "produced in %.1fs, drained in %.1fs  ->  %.0f tx/s\n", $ARGV[1]-$ARGV[0], $ARGV[2]-$ARGV[0], $ARGV[3]/($ARGV[2]-$ARGV[0])' \
  "$start" "$produced" "$end" "$N"
echo "$finished" | awk '{ printf "%-8s txs stored after the first:  p50 %ds  p95 %ds  last %ds\n", $1, $2, $3, $4 }'

awk -v n="$N" -v stored="$stored" -v sizes="$sizes" '
  function label(s, key) { return match(s, key "=\"[^\"]*\"") ? substr(s, RSTART + length(key) + 2, RLENGTH - length(key) - 3) : "" }
  function bound(le) { return le == "+Inf" ? 1e18 : le + 0 }
  function sortles(h,   i, j, t) {
    for (i = 2; i <= nle[h]; i++) for (j = i; j > 1 && bound(les[h, j-1]) > bound(les[h, j]); j--) {
      t = les[h, j]; les[h, j] = les[h, j-1]; les[h, j-1] = t
    }
  }
  function quantile(h, q,   total, rank, i, le, c, lo, plo) {
    total = cnt[h]; if (total == 0) return "-"
    rank = q * total; lo = 0; plo = 0
    for (i = 1; i <= nle[h]; i++) {
      le = les[h, i]; c = bucket[h, le]
      if (c >= rank) {
        if (le == "+Inf") return ">" lo
        return sprintf("%.9f", lo + (bound(le) - lo) * (c > plo ? (rank - plo) / (c - plo) : 0))
      }
      lo = bound(le); plo = c
    }
    return "-"
  }
  function addbucket(h, le, v) { if (!((h, le) in bucket)) les[h, ++nle[h]] = le; bucket[h, le] += v }

  FNR == 1 { inst = FILENAME; sub(/.*metrics-/, ":", inst); insts[++ni] = inst }
  /^#/ { next }
  {
    name = $1; labels = ""
    if (k = index(name, "{")) { labels = substr(name, k); name = substr(name, 1, k - 1) }
    v = $NF
  }
  name == "risk_txs_evaluated_total"      { risk[label(labels, "risk")] += v; done[inst] += v }
  name == "risk_txs_failed_through_total" { failed += v; done[inst] += v }
  name == "risk_txs_skipped_total"        { skipped += v; done[inst] += v }
  name == "risk_txs_dead_lettered_total"  { dead += v }
  name == "risk_rule_triggered_total"     { rules[label(labels, "rule")] += v }
  name == "risk_settle_failures_total"    { settle[label(labels, "kind")] += v }
  name == "process_cpu_seconds_total"     { cpu[inst] = v }
  name == "process_resident_memory_bytes" { rss[inst] = v }
  name == "go_sql_max_open_connections"   { maxopen = v }
  name == "go_sql_wait_count_total"       { waits += v }
  name == "go_sql_wait_duration_seconds_total" { waited += v }
  name == "risk_evaluation_stage_seconds_bucket" { addbucket(label(labels, "stage"), label(labels, "le"), v * 1) }
  name == "risk_evaluation_stage_seconds_sum"    { sum[label(labels, "stage")] += v }
  name == "risk_evaluation_stage_seconds_count"  { cnt[label(labels, "stage")] += v }

  END {
    printf "stored %d/%d   failed through %d   dead-lettered %d   skipped %d\n\n", stored, n, failed, dead, skipped

    printf "%-16s %8s %7s %8s %8s\n", "per instance", "txs", "share", "cpu", "memory"
    for (i = 1; i <= ni; i++) { x = insts[i]
      printf "  %-14s %8d %6.0f%% %7.1fs %6.0f MB\n", x, done[x], n ? 100 * done[x] / n : 0, cpu[x], rss[x] / 1048576 }

    split("load_history evaluate publish insert", order, " ")
    printf "\n%-16s %8s %7s %8s %8s\n", "stage (ms)", "avg", "p50", "p95", "p99"
    for (i = 1; i <= 4; i++) { h = order[i]; if (!cnt[h]) continue
      sortles(h)
      p50 = quantile(h, 0.5); p95 = quantile(h, 0.95); p99 = quantile(h, 0.99)
      printf "  %-14s %8.2f %7s %8s %8s\n", h, 1000 * sum[h] / cnt[h], ms(p50), ms(p95), ms(p99) }

    printf "db pool           max open %d   waits %d   wait time %.2fs\n", maxopen, waits, waited

    printf "risk             "; for (r in risk) if (risk[r]) printf "  %s %d", r, risk[r]; printf "\n"
    printf "rules triggered  "; any = 0; for (r in rules) if (rules[r]) { printf "  %s %d", r, rules[r]; any = 1 }; if (!any) printf "  none"; printf "\n"
    printf "settle failures  "; any = 0; for (k in settle) if (settle[k]) { printf "  %s %d", k, settle[k]; any = 1 }; if (!any) printf "  none"; printf "\n"
    printf "postgres          evaluated_txs %s\n", sizes
  }
  function ms(q) { return q ~ /^[0-9.]+$/ ? sprintf("%.2f", 1000 * q) : (q ~ /^>/ ? ">" sprintf("%.0f", 1000 * substr(q, 2)) : q) }
' "$TMP"/metrics-*
