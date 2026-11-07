#!/usr/bin/env bash
# Sends a few scenarios designed to trigger rules, then shows how Risk scored each one.
# Needs the local stack (make stack/up) and Risk running (make run).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
API=${RISK_URL:-http://localhost:4000}
TOPIC=${RISK_TOPIC:-txs.incoming}
RUN=demo-$(date +%s)
MSGS=$(mktemp)
trap 'rm -f "$MSGS"' EXIT

curl -fs "$API/v1/readycheck" >/dev/null ||
  { echo "Risk isn't ready at $API. Start it with: make run"; exit 1; }

ago() { perl -MPOSIX -e 'print strftime("%Y-%m-%dT%H:%M:%SZ", gmtime(time - $ARGV[0]))' "$1"; }
day=86400

# send account id json
send() { printf '%s|%s\n' "$1" "$2" >> "$MSGS"; }

card() { # account id amount mcc country category secondsAgo
  send "$1" "{\"id\":\"$2\",\"accountId\":\"$1\",\"transactionType\":\"card\",\"amount\":$3,\"postedAt\":\"$(ago "$7")\",\"category\":\"$6\",\"mcc\":\"$4\",\"paymentChannel\":\"online\",\"merchantName\":\"Demo Merchant\",\"merchantCountry\":\"$5\"}"
}

transfer() { # account id amount counterparty counterpartyType payeeAddedSecondsAgo payeeMatch device category secondsAgo
  send "$1" "{\"id\":\"$2\",\"accountId\":\"$1\",\"transactionType\":\"transfer\",\"amount\":$3,\"postedAt\":\"$(ago "${10}")\",\"category\":\"$9\",\"rail\":\"rtc\",\"counterpartyAccountHash\":\"$4\",\"counterpartyType\":\"$5\",\"payeeAddedAt\":\"$(ago "$6")\",\"payeeMatch\":\"$7\",\"deviceId\":\"$8\",\"initiationChannel\":\"app\"}"
}

debit_order() { # account id amount creditor sequence mandateMax category secondsAgo
  send "$1" "{\"id\":\"$2\",\"accountId\":\"$1\",\"transactionType\":\"debitOrder\",\"amount\":$3,\"postedAt\":\"$(ago "$8")\",\"category\":\"$7\",\"mandateId\":\"$2-mandate\",\"mandateType\":\"eft\",\"sequence\":\"$5\",\"creditorUserCode\":\"$4\",\"actionDate\":\"$(ago "$8" | cut -c1-10)\",\"mandateMaxAmount\":$6}"
}

scenarios=()
scenario() { scenarios+=("$1|$2"); }

a=$RUN-everyday
card "$a" "$a-1" -4500 5411 ZA groceries 60
scenario "Everyday grocery payment" "$a-1"

a=$RUN-mule
transfer "$a" "$a-1" 1500000 "$RUN-sender" EXTERNAL $((30 * day)) match phone-1 transfer_in 2400
transfer "$a" "$a-2" -1400000 "$RUN-new-payee" EXTERNAL 900 noMatch phone-1 transfer_out 300
scenario "Money in, then out to a payee added 10 min earlier" "$a-2"

a=$RUN-punter
card "$a" "$a-1" -600000 7995 ZA gambling 120
scenario "First gambling payment, R6,000" "$a-1"

a=$RUN-debit
debit_order "$a" "$a-1" -250000 "$RUN-crypto-co" represented 100000 cryptocurrency 180
scenario "Represented debit order above its mandate max" "$a-1"

a=$RUN-crypto
transfer "$a" "$a-1" -50000 "$RUN-exchange" EXTERNAL $((90 * day)) match phone-1 cryptocurrency 240
scenario "Transfer to a crypto exchange" "$a-1"

a=$RUN-refund
card "$a" "$a-1" 750000 5732 ZA electronics 60
scenario "Large card refund" "$a-1"

a=$RUN-takeover
for i in 1 2 3; do
  transfer "$a" "$a-$i" -20000 "$RUN-savings" OWN_ACCOUNT $((200 * day)) match phone-1 savings $((i * 7 * day))
done
transfer "$a" "$a-4" -900000 "$RUN-savings" OWN_ACCOUNT $((200 * day)) match unknown-laptop savings 60
scenario "Own-account transfer from a new device" "$a-4"

(cd "$ROOT" && docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
  --bootstrap-server localhost:19092 --topic "$TOPIC" \
  --reader-property parse.key=true --reader-property key.separator='|') < "$MSGS"
echo "Sent $(wc -l < "$MSGS" | tr -d ' ') transactions for run $RUN"
echo

printf '%-52s %-7s %-6s %s\n' SCENARIO RISK SCORE TRIGGERED
for s in "${scenarios[@]}"; do
  name=${s%%|*} id=${s#*|} result=""
  for _ in $(seq 1 30); do
    result=$(curl -fs "$API/v1/evaluated-txs/$id") && break
    sleep 1
  done
  [ -n "$result" ] || { printf '%-52s not stored after 30s\n' "$name"; continue; }
  IFS=$'\t' read -r risk score triggered < <(jq -r '.evaluatedTx |
    [.risk, .score, ([.ruleOutcomes[] | select(.triggered) | .rule] | join(", "))] | @tsv' <<< "$result")
  printf '%-52s %-7s %-6s %s\n' "$name" "$risk" "$score" "${triggered:--}"
done
echo
echo "Full results with reasons and evidence: curl -s $API/v1/evaluated-txs/<id> | jq"
