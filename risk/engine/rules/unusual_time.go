package rules

import (
	"time"

	"risk/risk"
	"risk/risk/engine"
)

type UnusualTime struct {
	NeighbourHours int
	MaxShare       float64
	MinBaselineTxs int
	ExcludeRecent  time.Duration
	Score          int
}

func (UnusualTime) SupportedKinds() []risk.TxKind { return risk.TxKinds }

func (r UnusualTime) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	hour := t.PostedAt.UTC().Hour()
	baselineEnd := t.PostedAt.Add(-r.ExcludeRecent)

	baselineTxs, txsNearHour := 0, 0
	for _, h := range ec.Profile.HoursBefore(baselineEnd) {
		baselineTxs += h.Count
		if hoursApart(h.Hour.UTC().Hour(), hour) <= r.NeighbourHours {
			txsNearHour += h.Count
		}
	}

	evidence := map[string]any{
		"hour":           hour,
		"neighbourHours": r.NeighbourHours,
		"txsNearHour":    txsNearHour,
		"baselineTxs":    baselineTxs,
		"minBaselineTxs": r.MinBaselineTxs,
		"maxShare":       r.MaxShare,
		"excludeRecent":  seconds(r.ExcludeRecent),
	}
	outcome := risk.RuleOutcome{Reason: risk.ReasonInsufficientHistory, Score: r.Score, Evidence: evidence}

	if baselineTxs < r.MinBaselineTxs {
		return outcome
	}
	share := float64(txsNearHour) / float64(baselineTxs)
	evidence["share"] = share
	outcome.Triggered = share < r.MaxShare
	outcome.Reason = pick(outcome.Triggered, risk.ReasonUnusualTime, risk.ReasonUsualTime)
	return outcome
}

func hoursApart(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	return min(d, 24-d)
}
