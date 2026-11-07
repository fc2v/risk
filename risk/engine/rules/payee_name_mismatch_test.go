package rules

import (
	"testing"

	"risk/risk"
)

func TestPayeeNameMismatch(t *testing.T) {
	r := PayeeNameMismatch{MismatchValues: []string{"noMatch"}, Score: 30}
	payeeMatch := func(v string) risk.Tx {
		tx := transfer(-10_000, now, "payee")
		tx.PayeeMatch = v
		return tx
	}

	runCases(t, r, []ruleCase{
		{name: "listed mismatch", tx: payeeMatch("noMatch"), want: risk.ReasonPayeeNameMismatch},
		{name: "match", tx: payeeMatch("match"), want: risk.ReasonPayeeNameNotFlagged},
		{name: "partial match not listed", tx: payeeMatch("partialMatch"), want: risk.ReasonPayeeNameNotFlagged},
		{name: "no check result", tx: payeeMatch(""), want: risk.ReasonPayeeNameNotFlagged},
		{name: "not a transfer", tx: card(-10_000, now), want: risk.ReasonNotApplicable},
	})
}
