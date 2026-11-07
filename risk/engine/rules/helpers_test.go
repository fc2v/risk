package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"risk/risk"
	"risk/risk/contract"
	"risk/risk/engine"
)

var now = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

type ruleCase struct {
	name  string
	tx    risk.Tx
	prior []risk.Tx
	want  risk.Reason
}

// Downstream relies on a reason always meaning the same triggered value.
var triggeringReasons = map[risk.Reason]bool{
	risk.ReasonVelocityAboveNormal:  true,
	risk.ReasonAmountAboveThreshold: true,
	risk.ReasonUnusualTime:          true,
	risk.ReasonFreshPayee:           true,
	risk.ReasonPayeeNameMismatch:    true,
	risk.ReasonPassThrough:          true,
	risk.ReasonNewCounterparty:      true,
	risk.ReasonNewDevice:            true,
	risk.ReasonNewCountry:           true,
	risk.ReasonNewCreditor:          true,
	risk.ReasonAboveMandateMax:      true,
	risk.ReasonRepresentedDebit:     true,
	risk.ReasonHighRiskMCC:          true,
	risk.ReasonHighRiskCategory:     true,
}

var producedReasons = map[risk.Reason]bool{}

func runCases(t *testing.T, r engine.Rule, cases []ruleCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Evaluate(tc.tx, engine.EvaluationContext{Profile: profile(tc.tx, tc.prior)})
			producedReasons[got.Reason] = true
			if got.Reason != tc.want || got.Triggered != triggeringReasons[tc.want] {
				t.Errorf("got %s (triggered %v), want %s (triggered %v); evidence %v",
					got.Reason, got.Triggered, tc.want, triggeringReasons[tc.want], got.Evidence)
			}
			checkEvidenceContract(t, got)
		})
	}
}

func profile(tx risk.Tx, prior []risk.Tx) engine.Profile {
	year := 365 * 24 * time.Hour
	return engine.BuildProfile(prior, engine.ProfileWindow{At: tx.PostedAt, Lookback: year, RecentWindow: year, LastAmounts: 1000})
}

// Checked on the published JSON, so a key encoded as the wrong type fails too.
func checkEvidenceContract(t *testing.T, o risk.RuleOutcome) {
	t.Helper()
	promised, ok := contract.PromisedEvidence[o.Reason]
	if !ok {
		t.Errorf("%s has no entry in contract.PromisedEvidence", o.Reason)
		return
	}

	raw, err := json.Marshal(contract.FromOutcome(o))
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		Evidence map[string]any `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}

	for key, want := range promised {
		v, ok := published.Evidence[key]
		if !ok {
			t.Errorf("%s promises evidence %q, missing from %v", o.Reason, key, published.Evidence)
			continue
		}
		if got := jsonType(v); got != want && !(want == contract.EvidenceNumber && got == contract.EvidenceInteger) {
			t.Errorf("%s evidence %q is %s (%v), promised %s", o.Reason, key, got, v, want)
		}
	}
}

func jsonType(v any) contract.EvidenceType {
	switch v := v.(type) {
	case float64:
		if v == math.Trunc(v) {
			return contract.EvidenceInteger
		}
		return contract.EvidenceNumber
	case bool:
		return contract.EvidenceBoolean
	case string:
		return contract.EvidenceString
	case []any:
		for _, e := range v {
			if _, ok := e.(string); !ok {
				return "array"
			}
		}
		return contract.EvidenceStrings
	default:
		return contract.EvidenceType(fmt.Sprintf("%T", v))
	}
}

func card(amount int64, at time.Time) risk.Tx {
	return risk.Tx{
		Type:     risk.TxTypeCard,
		Amount:   amount,
		PostedAt: at,
		Card:     &risk.Card{MCC: "5411", PaymentChannel: "in store", MerchantName: "Corner Shop", MerchantCountry: "ZA"},
	}
}

func transfer(amount int64, at time.Time, counterparty string) risk.Tx {
	return risk.Tx{
		Type:     risk.TxTypeTransfer,
		Amount:   amount,
		PostedAt: at,
		Transfer: &risk.Transfer{Rail: "rtc", CounterpartyAccountHash: counterparty, CounterpartyType: risk.CounterpartyExternal},
	}
}

func debitOrder(amount int64, at time.Time, creditor string) risk.Tx {
	return risk.Tx{
		Type:       risk.TxTypeDebitOrder,
		Amount:     amount,
		PostedAt:   at,
		DebitOrder: &risk.DebitOrder{MandateType: "eft", Sequence: "recurring", CreditorUserCode: creditor},
	}
}

// before returns n txs, the first one gap before from and each next one a further gap back.
func before(n int, gap time.Duration, from time.Time, build func(at time.Time) risk.Tx) []risk.Tx {
	txs := make([]risk.Tx, n)
	for i := range txs {
		txs[i] = build(from.Add(-time.Duration(i+1) * gap))
	}
	return txs
}
