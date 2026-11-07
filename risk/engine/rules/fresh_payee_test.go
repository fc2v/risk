package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestFreshPayee(t *testing.T) {
	r := FreshPayee{MaxPayeeAge: time.Hour, MinAmount: 100_000, Score: 35}
	payeeAdded := func(amount int64, ago time.Duration) risk.Tx {
		tx := transfer(amount, now, "payee")
		tx.PayeeAddedAt = new(now.Add(-ago))
		return tx
	}

	runCases(t, r, []ruleCase{
		{name: "payee added minutes ago", tx: payeeAdded(-150_000, 10*time.Minute), want: risk.ReasonFreshPayee},
		{name: "payee exactly max age", tx: payeeAdded(-150_000, time.Hour), want: risk.ReasonFreshPayee},
		{name: "payee older than max age", tx: payeeAdded(-150_000, 2*time.Hour), want: risk.ReasonPayeeNotFresh},
		{name: "amount below minimum", tx: payeeAdded(-50_000, 10*time.Minute), want: risk.ReasonAmountBelowMinimum},
		{name: "payee added time unknown", tx: transfer(-150_000, now, "payee"), want: risk.ReasonPayeeAgeUnknown},
		{name: "not a transfer", tx: card(-150_000, now), want: risk.ReasonNotApplicable},
	})
}
