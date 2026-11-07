package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestHighAmount(t *testing.T) {
	r := HighAmount{Multiplier: 3, MinBaselineTxs: 5, MedianOfLast: 200, DefaultThreshold: 500_000, Score: 30}
	spends := func(amounts ...int64) []risk.Tx {
		txs := make([]risk.Tx, len(amounts))
		for i, a := range amounts {
			txs[i] = card(a, now.Add(-time.Duration(i+1)*24*time.Hour))
		}
		return txs
	}
	usual := spends(-10_000, -10_000, -10_000, -10_000, -10_000)

	runCases(t, r, []ruleCase{
		{name: "new account above default", tx: card(-600_000, now), prior: spends(-10_000, -10_000), want: risk.ReasonAmountAboveThreshold},
		{name: "new account below default", tx: card(-400_000, now), prior: spends(-10_000, -10_000), want: risk.ReasonAmountWithinThreshold},
		{name: "above multiplier x median", tx: card(-40_000, now), prior: usual, want: risk.ReasonAmountAboveThreshold},
		{name: "exactly at threshold", tx: card(-30_000, now), prior: usual, want: risk.ReasonAmountWithinThreshold},
		{
			name:  "even count uses mean of middle two",
			tx:    card(-110_000, now),
			prior: spends(-10_000, -20_000, -30_000, -40_000, -50_000, -60_000),
			want:  risk.ReasonAmountAboveThreshold,
		},
		{
			name:  "even count below threshold",
			tx:    card(-100_000, now),
			prior: spends(-10_000, -20_000, -30_000, -40_000, -50_000, -60_000),
			want:  risk.ReasonAmountWithinThreshold,
		},
		{
			name:  "one-off large purchase doesn't raise the median",
			tx:    card(-40_000, now),
			prior: spends(-10_000, -10_000, -10_000, -10_000, -1_000_000),
			want:  risk.ReasonAmountAboveThreshold,
		},
		{name: "large refund compares by size", tx: card(40_000, now), prior: usual, want: risk.ReasonAmountAboveThreshold},
	})
}
