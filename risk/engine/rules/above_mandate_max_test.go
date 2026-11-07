package rules

import (
	"testing"

	"risk/risk"
)

func TestAboveMandateMax(t *testing.T) {
	r := AboveMandateMax{Score: 40}
	withMax := func(amount, mandateMax int64) risk.Tx {
		tx := debitOrder(amount, now, "creditor")
		tx.MandateMaxAmount = new(mandateMax)
		return tx
	}

	runCases(t, r, []ruleCase{
		{name: "above max", tx: withMax(-15_000, 10_000), want: risk.ReasonAboveMandateMax},
		{name: "at max", tx: withMax(-10_000, 10_000), want: risk.ReasonWithinMandateMax},
		{name: "below max", tx: withMax(-5_000, 10_000), want: risk.ReasonWithinMandateMax},
		{name: "max unknown", tx: debitOrder(-15_000, now, "creditor"), want: risk.ReasonMandateMaxUnknown},
		{name: "not a debit order", tx: card(-15_000, now), want: risk.ReasonNotApplicable},
	})
}
