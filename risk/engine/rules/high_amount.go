package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type HighAmount struct {
	Multiplier       float64
	MinBaselineTxs   int
	MedianOfLast     int
	DefaultThreshold int64
	Score            int
}

func (HighAmount) SupportedKinds() []risk.TxKind { return risk.TxKinds }

func (r HighAmount) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	baselineTxs := ec.Profile.TxCount()
	sizes := lastSizes(ec.Profile.Amounts, r.MedianOfLast)
	usedDefault := baselineTxs < r.MinBaselineTxs || len(sizes) == 0
	evidence := map[string]any{
		"usedDefault":    usedDefault,
		"baselineTxs":    baselineTxs,
		"medianOfLast":   r.MedianOfLast,
		"minBaselineTxs": r.MinBaselineTxs,
		"multiplier":     r.Multiplier,
	}

	threshold := float64(r.DefaultThreshold)
	if !usedDefault {
		median := median(sizes)
		evidence["medianSize"] = median
		threshold = median * r.Multiplier
	}
	evidence["threshold"] = threshold

	triggered := float64(size(t.Amount)) > threshold
	return risk.RuleOutcome{
		Triggered: triggered,
		Reason:    pick(triggered, risk.ReasonAmountAboveThreshold, risk.ReasonAmountWithinThreshold),
		Score:     r.Score,
		Evidence:  evidence,
	}
}

func lastSizes(amounts []engine.RecentAmount, n int) []int64 {
	amounts = slices.Clone(amounts)
	slices.SortFunc(amounts, func(a, b engine.RecentAmount) int { return b.PostedAt.Compare(a.PostedAt) })
	sizes := make([]int64, 0, min(n, len(amounts)))
	for _, a := range amounts[:min(n, len(amounts))] {
		sizes = append(sizes, a.Size)
	}
	return sizes
}

func median(sizes []int64) float64 {
	sizes = slices.Clone(sizes)
	slices.Sort(sizes)
	mid := len(sizes) / 2
	if len(sizes)%2 == 1 {
		return float64(sizes[mid])
	}
	return (float64(sizes[mid-1]) + float64(sizes[mid])) / 2
}
