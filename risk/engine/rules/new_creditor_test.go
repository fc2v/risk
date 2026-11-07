package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestNewCreditor(t *testing.T) {
	r := NewCreditor{MandateTypes: []string{"eft"}, MinBaselineTxs: 1, Score: 30}
	debit := func(creditor, mandateType string) risk.Tx {
		tx := debitOrder(-10_000, now, creditor)
		tx.MandateType = mandateType
		return tx
	}
	debitedByC1 := []risk.Tx{debitOrder(-10_000, now.Add(-30*24*time.Hour), "c1")}

	runCases(t, r, []ruleCase{
		{name: "new creditor", tx: debit("c2", "eft"), prior: debitedByC1, want: risk.ReasonNewCreditor},
		{name: "known creditor", tx: debit("c1", "eft"), prior: debitedByC1, want: risk.ReasonKnownCreditor},
		{name: "unlisted mandate type", tx: debit("c2", "debicheck"), prior: debitedByC1, want: risk.ReasonMandateTypeNotChecked},
		{name: "no history", tx: debit("c2", "eft"), want: risk.ReasonInsufficientHistory},
		{name: "not a debit order", tx: card(-10_000, now), prior: debitedByC1, want: risk.ReasonNotApplicable},
	})
}
