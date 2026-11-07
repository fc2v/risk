package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestHighRiskMCC(t *testing.T) {
	mccs := []string{"7995", "6051"}
	mcc := func(code string, at time.Time) risk.Tx {
		tx := card(-10_000, at)
		tx.MCC = code
		return tx
	}
	groceriesOnly := []risk.Tx{mcc("5411", now.Add(-24*time.Hour))}
	gambledBefore := append(groceriesOnly, mcc("7995", now.Add(-48*time.Hour)))

	t.Run("first time only", func(t *testing.T) {
		runCases(t, HighRiskMCC{MCCs: mccs, FirstTimeOnly: true, Score: 20}, []ruleCase{
			{name: "first listed mcc", tx: mcc("7995", now), prior: groceriesOnly, want: risk.ReasonHighRiskMCC},
			{name: "any listed mcc before", tx: mcc("6051", now), prior: gambledBefore, want: risk.ReasonHighRiskMCCSeenBefore},
			{name: "unlisted mcc", tx: mcc("5411", now), prior: groceriesOnly, want: risk.ReasonMCCNotHighRisk},
			{name: "not a card tx", tx: transfer(-10_000, now, "payee"), want: risk.ReasonNotApplicable},
		})
	})

	t.Run("every time", func(t *testing.T) {
		runCases(t, HighRiskMCC{MCCs: mccs, Score: 20}, []ruleCase{
			{name: "listed mcc after earlier ones", tx: mcc("6051", now), prior: gambledBefore, want: risk.ReasonHighRiskMCC},
			{name: "unlisted mcc", tx: mcc("5411", now), prior: gambledBefore, want: risk.ReasonMCCNotHighRisk},
		})
	})
}
