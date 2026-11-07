package rules

import (
	"testing"

	"risk/risk"
)

func TestRepresentedDebit(t *testing.T) {
	r := RepresentedDebit{Sequences: []string{"represented"}, Score: 15}
	sequence := func(v string) risk.Tx {
		tx := debitOrder(-10_000, now, "creditor")
		tx.Sequence = v
		return tx
	}

	runCases(t, r, []ruleCase{
		{name: "represented", tx: sequence("represented"), want: risk.ReasonRepresentedDebit},
		{name: "recurring", tx: sequence("recurring"), want: risk.ReasonNotRepresented},
		{name: "first", tx: sequence("first"), want: risk.ReasonNotRepresented},
		{name: "not a debit order", tx: card(-10_000, now), want: risk.ReasonNotApplicable},
	})
}
