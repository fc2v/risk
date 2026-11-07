package rules

import (
	"testing"

	"risk/risk"
)

func TestHighRiskCategory(t *testing.T) {
	categorised := func(tx risk.Tx, category string) risk.Tx {
		tx.Category = category
		return tx
	}
	r := HighRiskCategory{Categories: []string{"gambling", "cryptocurrency"}, Score: 15}

	runCases(t, r, []ruleCase{
		{name: "listed category", tx: categorised(transfer(-10_000, now, "payee"), "gambling"), want: risk.ReasonHighRiskCategory},
		{name: "listed category on a debit order", tx: categorised(debitOrder(-10_000, now, "creditor"), "cryptocurrency"), want: risk.ReasonHighRiskCategory},
		{name: "unlisted category", tx: categorised(transfer(-10_000, now, "payee"), "groceries"), want: risk.ReasonCategoryNotHighRisk},
		{name: "match is exact", tx: categorised(transfer(-10_000, now, "payee"), "Gambling"), want: risk.ReasonCategoryNotHighRisk},
		{name: "no category", tx: transfer(-10_000, now, "payee"), want: risk.ReasonNotApplicable},
	})
}
