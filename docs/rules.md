# Rules

How Risk scores a transaction, and every rule it can run.

## How Scoring Works

1. The transaction's **kind** is derived from its type and amount.
2. Every enabled rule instance whose `appliesTo` includes that kind is evaluated against the account's history.
3. Each rule sees only the history transactions whose kinds are in its `learnsFrom` (default: its `appliesTo`), posted within `history.lookback` before this one.
4. The scores of the rules that triggered are added up and capped at 100.
5. The total is banded: below `minMedium` is `LOW`, from `minMedium` is `MEDIUM`, from `minHigh` is `HIGH`.
6. If no rule applies to the kind, the risk is `UNKNOWN` with score 0.

### Kinds

| Kind | Transaction |
|---|---|
| `card` | Card payment (amount negative) |
| `cardRefund` | Card refund (amount positive) |
| `debitOrder` | Debit order |
| `transferOut` | Transfer to an external account (amount negative) |
| `transferIn` | Transfer from an external account (amount positive) |
| `ownAccountTransfer` | Transfer between the customer's own accounts (`counterpartyType: OWN_ACCOUNT`), either direction |

### Rule Instances

`rules` in the config is a list of named instances. The same rule can run more than once with different settings, for example one `highAmount` for spending and another for incoming money:

```json
{ "name": "highAmountSpend",   "rule": "highAmount", "enabled": true, "score": 30,
  "appliesTo": ["card", "transferOut"], "multiplier": 3, "minBaselineTxs": 5, "defaultThreshold": 500000 },
{ "name": "highAmountInbound", "rule": "highAmount", "enabled": true, "score": 20,
  "appliesTo": ["transferIn"], "multiplier": 5, "minBaselineTxs": 3, "defaultThreshold": 2000000 }
```

| Field | Description |
|---|---|
| `name` | Unique, and shown as `rule` in results and in metrics |
| `rule` | Which rule, from the list below |
| `enabled` | Disabled instances are skipped and not validated |
| `score` | 0–100, added when the rule triggers |
| `appliesTo` | Kinds the instance judges, which must be kinds the rule supports |
| `learnsFrom` | Kinds that form its history baseline. Any kinds are allowed, and it defaults to `appliesTo` |

The remaining fields are the rule's parameters. A parameter belonging to a different rule is rejected.

Combine small rules instead of writing compound ones: a large inflow from a new sender can be `highAmountInbound` plus `newCounterparty`, with scores tuned so that only both together reach `MEDIUM`.

### Results

Every evaluated rule produces an outcome:

```json
{ "rule": "freshPayee", "triggered": true, "reason": "FRESH_PAYEE", "score": 35,
  "evidence": { "maxPayeeAge": 3600, "minAmount": 100000, "payeeAge": 900 } }
```

`reason` says why the rule did or didn't trigger, and `evidence` holds the values it decided on. Durations are whole seconds, amounts are minor units. A rule given a transaction without the details it needs reports `NOT_APPLICABLE`. The [Message Contract](contract.md#reason-codes) lists the evidence each reason guarantees.

## Rules

Amounts are compared by size, so refunds and inflows count as much as spending. Rules that need history report `INSUFFICIENT_HISTORY` until the baseline is large enough.

### `velocity`

More transactions in a short window than the account's normal rate. The window is counted exactly. The baseline is the history before it, ending on a whole hour, so a burst can't raise its own expectation.

Supports all kinds.

| Parameter | Description |
|---|---|
| `window` | Window to count, e.g. `"1h"` |
| `multiplier` | Triggers when the count exceeds this many times the expected count |
| `minCount` | And the count is at least this, this transaction included |
| `minBaselineTxs` | History needed before the rule judges |
| `minBaselineSpan` | Minimum time the baseline must cover, e.g. `"168h"` |

Reasons: `VELOCITY_ABOVE_NORMAL`, `VELOCITY_WITHIN_NORMAL`, `INSUFFICIENT_HISTORY`.

### `highAmount`

Amount far above the account's typical size. Uses the median of the most recent amounts, so one large purchase doesn't raise the bar.

Supports all kinds.

| Parameter | Description |
|---|---|
| `multiplier` | Triggers above this many times the median size of the baseline |
| `minBaselineTxs` | Below this many baseline transactions, `defaultThreshold` is used instead |
| `medianOfLast` | The median is taken over the account's last this many transactions, e.g. `200`. It must be at least `minBaselineTxs` |
| `defaultThreshold` | Fixed threshold in minor units for accounts with little history |

Reasons: `AMOUNT_ABOVE_THRESHOLD`, `AMOUNT_WITHIN_THRESHOLD`.

### `unusualTime`

Posted at an hour the account rarely transacts. Hours are UTC. The rule learns each account's own pattern, so no time zone is needed.

Supports all kinds.

| Parameter | Description |
|---|---|
| `neighbourHours` | Hours either side of this transaction's hour that count as "near" |
| `maxShare` | Triggers when fewer than this share of baseline transactions were near this hour |
| `minBaselineTxs` | History needed before the rule judges |
| `excludeRecent` | Recent history left out, so a new habit isn't judged by itself, e.g. `"24h"`. The baseline ends on the whole hour before |

Reasons: `UNUSUAL_TIME`, `USUAL_TIME`, `INSUFFICIENT_HISTORY`.

### `freshPayee`

Payment to a payee added moments before.

Supports `transferOut`.

| Parameter | Description |
|---|---|
| `maxPayeeAge` | Triggers when the payee was added at most this long before posting, e.g. `"1h"` |
| `minAmount` | And the amount is at least this |

Reasons: `FRESH_PAYEE`, `PAYEE_NOT_FRESH`, `AMOUNT_BELOW_MINIMUM`, `PAYEE_AGE_UNKNOWN`.

### `payeeNameMismatch`

The bank's payee-name check didn't match the account holder.

Supports `transferOut`.

| Parameter | Description |
|---|---|
| `mismatchValues` | `payeeMatch` values that trigger, e.g. `["noMatch"]` |

Reasons: `PAYEE_NAME_MISMATCH`, `PAYEE_NAME_NOT_FLAGGED`.

### `passThrough`

Money received and quickly sent on, the typical pattern of a mule account. Outflows since the first inflow are added together, so splitting the money across several payees still triggers.

Supports `transferOut`. Set `learnsFrom` to include `transferIn` (and `ownAccountTransfer`), or it never sees inflows.

| Parameter | Description |
|---|---|
| `window` | How far back to look for inflows, e.g. `"6h"`. It must fit within `history.lookback` |
| `minInbound` | Inflows in the window must total at least this |
| `minShare` | Triggers when outflows since the first inflow reach this share of the inflows, e.g. `0.8` |
| `newCounterpartyOnly` | Only trigger when paying someone not paid before |

Reasons: `PASS_THROUGH`, `INFLOW_BELOW_MINIMUM`, `SHARE_BELOW_MINIMUM`, `PAYEE_PAID_BEFORE`.

### `newCounterparty`

First payment to, or from, a counterparty.

Supports `transferOut`, `transferIn`.

| Parameter | Description |
|---|---|
| `minBaselineTxs` | History needed before the rule judges |

Reasons: `NEW_COUNTERPARTY`, `KNOWN_COUNTERPARTY`, `INSUFFICIENT_HISTORY`.

### `newDevice`

Transfer from a device the account hasn't used.

Supports `transferOut`, `ownAccountTransfer`.

| Parameter | Description |
|---|---|
| `minBaselineTxs` | History with a known device needed before the rule judges |

Reasons: `NEW_DEVICE`, `KNOWN_DEVICE`, `DEVICE_UNKNOWN`, `INSUFFICIENT_HISTORY`.

### `newCountry`

Card used in a country the account hasn't used it in.

Supports `card`, `cardRefund`.

| Parameter | Description |
|---|---|
| `paymentChannels` | Channels checked, e.g. `["in store", "online"]` |
| `minBaselineTxs` | History needed before the rule judges |

Reasons: `NEW_COUNTRY`, `KNOWN_COUNTRY`, `CHANNEL_NOT_CHECKED`, `INSUFFICIENT_HISTORY`.

### `newCreditor`

Debit order from a creditor that hasn't debited the account before.

Supports `debitOrder`.

| Parameter | Description |
|---|---|
| `mandateTypes` | Mandate types checked, e.g. `["eft"]` |
| `minBaselineTxs` | History needed before the rule judges |

Reasons: `NEW_CREDITOR`, `KNOWN_CREDITOR`, `MANDATE_TYPE_NOT_CHECKED`, `INSUFFICIENT_HISTORY`.

### `aboveMandateMax`

Debit order above the maximum the mandate allows.

Supports `debitOrder`. No parameters.

Reasons: `ABOVE_MANDATE_MAX`, `WITHIN_MANDATE_MAX`, `MANDATE_MAX_UNKNOWN`.

### `representedDebit`

A debit order presented again after failing.

Supports `debitOrder`.

| Parameter | Description |
|---|---|
| `sequences` | `sequence` values that trigger, e.g. `["represented"]` |

Reasons: `REPRESENTED_DEBIT`, `NOT_REPRESENTED`.

### `highRiskMCC`

Card payment at a high-risk merchant category, such as gambling, crypto or money transfer.

Supports `card`, `cardRefund`.

| Parameter | Description |
|---|---|
| `mccs` | Merchant category codes, 4 digits each |
| `firstTimeOnly` | Only trigger when the account has no earlier transaction at any listed code |

Reasons: `HIGH_RISK_MCC`, `HIGH_RISK_MCC_SEEN_BEFORE`, `MCC_NOT_HIGH_RISK`.

### `highRiskCategory`

Transaction in a high-risk category, such as gambling or cryptocurrency, using the producer's `category` field. Values must match exactly, so the list has to be agreed with the producer.

Supports all kinds. The shipped config applies it to outgoing transfers and debit orders. Card payments are left to `highRiskMCC`, because a card's category usually comes from its MCC and scoring both would count the same signal twice.

| Parameter | Description |
|---|---|
| `categories` | Categories that trigger, e.g. `["gambling", "cryptocurrency"]` |

Reasons: `HIGH_RISK_CATEGORY`, `CATEGORY_NOT_HIGH_RISK`. A transaction without a `category` reports `NOT_APPLICABLE`.

## Tuning

- Scores and thresholds in [`config.json`](../config.json) are starting points, not calibrated values.
- `history.lookback` sets how much history each evaluation reads. A shorter lookback means fewer hourly summaries to load per transaction.
- Watch `risk_rule_triggered_total` by rule. A rule that fires on most transactions adds noise, and one that never fires may be misconfigured.
