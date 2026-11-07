package rules

import (
	"fmt"
	"testing"
	"time"

	"risk/risk"
)

func TestNewCounterparty(t *testing.T) {
	r := NewCounterparty{MinBaselineTxs: 5, Score: 15}
	paid := func(n int) []risk.Tx {
		txs := make([]risk.Tx, n)
		for i := range txs {
			txs[i] = transfer(-1000, now.Add(-time.Duration(i+1)*24*time.Hour), fmt.Sprintf("known-%d", i))
		}
		return txs
	}

	runCases(t, r, []ruleCase{
		{name: "new payee", tx: transfer(-1000, now, "new"), prior: paid(5), want: risk.ReasonNewCounterparty},
		{name: "known payee", tx: transfer(-1000, now, "known-2"), prior: paid(5), want: risk.ReasonKnownCounterparty},
		{name: "too few baseline txs", tx: transfer(-1000, now, "new"), prior: paid(4), want: risk.ReasonInsufficientHistory},
		{name: "new sender", tx: transfer(1000, now, "new"), prior: paid(5), want: risk.ReasonNewCounterparty},
		{name: "not a transfer", tx: card(-1000, now), prior: paid(5), want: risk.ReasonNotApplicable},
	})
}
