# Message Contract

What Risk expects from the producer, and what it publishes for downstream systems.

## Overview

```
producer ──▶ txs.incoming ──▶ risk ──▶ txs.evaluated ──▶ downstream
                                 └──▶ GET /v1/evaluated-txs (same shape, plus evaluatedAt)
```

| Topic | Key | Value |
|---|---|---|
| `txs.incoming` | Account ID | One transaction, JSON |
| `txs.evaluated` | Account ID | The transaction plus its risk result, JSON, with a `tx-id` header |

Keying by account keeps each account's transactions on one partition, so they are evaluated and published in order.

Risk only assesses. A result is an assessment, not a decision: each subscriber decides what a score or risk band means for it, and whether and how to act, for example holding funds, contacting the customer or opening a case.

## Conventions

- **Amounts** are integers in minor units (cents). Negative is money out of the account. All accounts use one currency.
- **Times** are RFC 3339. Incoming times may carry any offset, and published times are UTC.
- **Durations** in evidence are whole seconds.
- **Hours** in evidence are UTC.

## Incoming Transactions

Samples: [card](../risk/contract/testdata/incoming_tx_card.json), [debit order](../risk/contract/testdata/incoming_tx_debit_order.json), [transfer](../risk/contract/testdata/incoming_tx_transfer.json).

### All Types

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | string | yes | Unique per transaction. Resending the same ID with the same content is safe |
| `accountId` | string | yes | Pseudonymous account identifier, also used as the message key |
| `transactionType` | string | yes | `card`, `debitOrder` or `transfer` |
| `amount` | integer | yes | Minor units, not zero |
| `postedAt` | time | yes | When the transaction posted |
| `category` | string | no | |

### Card

| Field | Type | Required | Notes |
|---|---|---|---|
| `mcc` | string | yes | 4 digits |
| `paymentChannel` | string | yes | e.g. `in store`, `online` |
| `merchantName` | string | yes | |
| `merchantCountry` | string | yes | ISO 3166-1 alpha-2, e.g. `ZA` |
| `merchantCity`, `merchantProvince`, `merchantPostalCode` | string | no | |

### Debit Order

| Field | Type | Required | Notes |
|---|---|---|---|
| `mandateId` | string | yes | |
| `mandateType` | string | yes | e.g. `eft`, `debicheck` |
| `sequence` | string | yes | e.g. `first`, `recurring`, `represented` |
| `creditorUserCode` | string | yes | Identifies the creditor |
| `actionDate` | string | yes | `YYYY-MM-DD` |
| `mandateCreatedAt` | time | no | |
| `mandateMaxAmount` | integer | no | Minor units |
| `frequency`, `creditorName`, `creditorBankCode` | string | no | |

### Transfer

| Field | Type | Required | Notes |
|---|---|---|---|
| `rail` | string | yes | e.g. `eft`, `rtc` |
| `counterpartyAccountHash` | string | yes | Stable hash identifying the other account |
| `counterpartyType` | string | yes | `OWN_ACCOUNT` or `EXTERNAL` |
| `initiationChannel` | string | no | e.g. `app` |
| `deviceId` | string | no | Device that initiated it |
| `counterpartyName`, `counterpartyBankCode` | string | no | |
| `counterpartyProxy` | string | no | e.g. a phone number used to pay |
| `payeeAddedAt` | time | no | When the payee was added |
| `payeeMatch` | string | no | Result of the payee name check, e.g. `match`, `partialMatch`, `noMatch` |
| `reference` | string | no | |

Fields not listed are ignored. Some rules compare fields against configured values (`category`, `paymentChannel`, `mandateType`, `sequence`, `payeeMatch`), so changes to the values the producer sends need to be agreed.

### Invalid Messages

A message that isn't valid JSON, is missing a required field or has a malformed one is not evaluated. Risk publishes it as `UNKNOWN` with `failureReason: INVALID` and keeps a copy for investigation. Send a corrected message with the same `id`.

## Published Results

Samples: [card](../risk/contract/testdata/evaluated_tx_card.json), [debit order](../risk/contract/testdata/evaluated_tx_debit_order.json), [transfer](../risk/contract/testdata/evaluated_tx_transfer.json), [failure](../risk/contract/testdata/evaluated_tx_failure.json).

Each message is the incoming transaction's fields (times in UTC) plus:

| Field | Type | Notes |
|---|---|---|
| `score` | integer | 0–100: sum of the triggered rules' scores, capped |
| `risk` | string | `LOW`, `MEDIUM`, `HIGH` or `UNKNOWN` |
| `ruleOutcomes` | array | One entry per rule evaluated, empty when none applied or evaluation failed |
| `failureReason` | string | Only when evaluation failed: `INVALID`, `CONFLICT` or `INTERNAL` |

The API returns the same object with `evaluatedAt`, when Risk stored it.

```json
{
  "id": "9b2e7c41-0d3f-4a8e-b6c5-1f7a2d9e3c08",
  "accountId": "3f6c1a52-9d4e-4b7a-8f21-6e0d2c9b7a14",
  "transactionType": "transfer",
  "amount": -450000,
  "postedAt": "2026-11-09T02:10:00Z",
  "rail": "rtc",
  "counterpartyAccountHash": "e40f7b",
  "counterpartyType": "EXTERNAL",
  "payeeAddedAt": "2026-11-09T01:55:00Z",
  "score": 65,
  "risk": "HIGH",
  "ruleOutcomes": [
    { "rule": "freshPayee", "triggered": true, "reason": "FRESH_PAYEE", "score": 35,
      "evidence": { "maxPayeeAge": 3600, "minAmount": 100000, "payeeAge": 900 } }
  ]
}
```

### `UNKNOWN`

| `failureReason` | Meaning |
|---|---|
| absent | Evaluated, but no rule applies to this kind of transaction |
| `INVALID` | The incoming message was invalid |
| `CONFLICT` | A different transaction with the same `id` was already evaluated, and the original stands |
| `INTERNAL` | Risk couldn't evaluate it, but it may be evaluated later |

Treat `UNKNOWN` with a `failureReason` as not assessed, not as low risk.

### Rule Outcomes

| Field | Type | Notes |
|---|---|---|
| `rule` | string | Name of the rule instance, e.g. `highAmountSpend`. Instances can be renamed or added |
| `triggered` | boolean | Whether it added its score |
| `reason` | string | Why it did or didn't trigger, listed below |
| `score` | integer | Score it adds when triggered |
| `evidence` | object | Values the rule decided on |

Base presentation and logic on `reason` and `evidence`, not on `rule`, which comes from configuration.

Risk publishes codes and values, not sentences. Turning them into language is up to each downstream service, because a fraud analyst, a customer message and a case note need different words. For example, `FRESH_PAYEE` with `payeeAge` 900 and `maxPayeeAge` 3600 could read "paid a payee added 15 minutes earlier".

## Reason Codes

Every reason guarantees the evidence keys listed. Outcomes may carry more keys, but only these are promised. A reason always means the same `triggered` value.

Types: *int* is an integer, *num* any number, *bool* a boolean, *str* a string, *strs* an array of strings.

### Shared

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `NOT_APPLICABLE` | no | none |
| `INSUFFICIENT_HISTORY` | no | `baselineTxs` int, `minBaselineTxs` int |

### Velocity

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `VELOCITY_ABOVE_NORMAL` | yes | `countInWindow` int, `window` int, `minCount` int, `multiplier` num, `baselineTxs` int, `minBaselineTxs` int, `baselineSpan` int, `expectedInWindow` num |
| `VELOCITY_WITHIN_NORMAL` | no | same as above |

### Amount

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `AMOUNT_ABOVE_THRESHOLD` | yes | `threshold` num, `multiplier` num, `usedDefault` bool, `baselineTxs` int, `minBaselineTxs` int |
| `AMOUNT_WITHIN_THRESHOLD` | no | same as above |

`medianSize` (num) is present when `usedDefault` is false: the median size of the account's last `medianOfLast` transactions.

### Time of Day

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `UNUSUAL_TIME` | yes | `hour` int, `neighbourHours` int, `txsNearHour` int, `share` num, `maxShare` num, `baselineTxs` int, `minBaselineTxs` int, `excludeRecent` int |
| `USUAL_TIME` | no | same as above |

### Fresh Payee

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `FRESH_PAYEE` | yes | `payeeAge` int, `maxPayeeAge` int, `minAmount` int |
| `PAYEE_NOT_FRESH` | no | `payeeAge` int, `maxPayeeAge` int, `minAmount` int |
| `AMOUNT_BELOW_MINIMUM` | no | `payeeAge` int, `maxPayeeAge` int, `minAmount` int |
| `PAYEE_AGE_UNKNOWN` | no | `maxPayeeAge` int, `minAmount` int |

### Payee Name

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `PAYEE_NAME_MISMATCH` | yes | `payeeMatch` str, `mismatchValues` strs |
| `PAYEE_NAME_NOT_FLAGGED` | no | `payeeMatch` str, `mismatchValues` strs |

### Pass-Through

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `PASS_THROUGH` | yes | `inboundInWindow` int, `outboundSinceInflow` int, `share` num, `minShare` num, `minInbound` int, `window` int, `paidBefore` bool, `newCounterpartyOnly` bool |
| `SHARE_BELOW_MINIMUM` | no | same as above |
| `PAYEE_PAID_BEFORE` | no | same as above |
| `INFLOW_BELOW_MINIMUM` | no | `inboundInWindow` int, `outboundSinceInflow` int, `minShare` num, `minInbound` int, `window` int, `paidBefore` bool, `newCounterpartyOnly` bool |

### New Counterparty, Device, Country, Creditor

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `NEW_COUNTERPARTY` | yes | `priorCounterparties` int, `baselineTxs` int, `minBaselineTxs` int |
| `KNOWN_COUNTERPARTY` | no | same as above |
| `NEW_DEVICE` | yes | `priorDevices` int, `baselineTxs` int, `minBaselineTxs` int |
| `KNOWN_DEVICE` | no | same as above |
| `DEVICE_UNKNOWN` | no | `priorDevices` int, `baselineTxs` int, `minBaselineTxs` int |
| `NEW_COUNTRY` | yes | `merchantCountry` str, `paymentChannel` str, `paymentChannels` strs, `priorCountries` int, `baselineTxs` int, `minBaselineTxs` int |
| `KNOWN_COUNTRY` | no | same as above |
| `CHANNEL_NOT_CHECKED` | no | `merchantCountry` str, `paymentChannel` str, `paymentChannels` strs, `priorCountries` int, `baselineTxs` int, `minBaselineTxs` int |
| `NEW_CREDITOR` | yes | `creditorUserCode` str, `mandateType` str, `sequence` str, `mandateTypes` strs, `priorCreditors` int, `baselineTxs` int, `minBaselineTxs` int |
| `KNOWN_CREDITOR` | no | same as above |
| `MANDATE_TYPE_NOT_CHECKED` | no | `creditorUserCode` str, `mandateType` str, `sequence` str, `mandateTypes` strs, `priorCreditors` int, `baselineTxs` int, `minBaselineTxs` int |

### Debit Orders

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `ABOVE_MANDATE_MAX` | yes | `mandateMaxAmount` int |
| `WITHIN_MANDATE_MAX` | no | `mandateMaxAmount` int |
| `MANDATE_MAX_UNKNOWN` | no | none |
| `REPRESENTED_DEBIT` | yes | `sequence` str, `sequences` strs |
| `NOT_REPRESENTED` | no | `sequence` str, `sequences` strs |

### Merchant Category

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `HIGH_RISK_MCC` | yes | `mcc` str, `mccs` strs |
| `HIGH_RISK_MCC_SEEN_BEFORE` | no | `mcc` str, `mccs` strs, `priorHighRiskMCCs` int |
| `MCC_NOT_HIGH_RISK` | no | `mcc` str, `mccs` strs |

### Transaction Category

| Reason | Triggered | Guaranteed evidence |
|---|---|---|
| `HIGH_RISK_CATEGORY` | yes | `category` str, `categories` strs |
| `CATEGORY_NOT_HIGH_RISK` | no | `category` str, `categories` strs |

## Guarantees

- **At least once:** a transaction may be published more than once, for example after a restart. Deduplicate on the `tx-id` header (equal to `id`).
- **Later replaces earlier:** a later message for the same `tx-id` replaces an earlier `UNKNOWN` one, for example after a failed transaction is replayed.
- **Ordered per account:** an account's results are published in the order its transactions arrived.
- **Codes only grow:** new reason codes, failure reasons and fields may be added without notice, so handle unknown ones with a generic fallback. Existing codes never change meaning, and guaranteed evidence keys are never removed or retyped.
