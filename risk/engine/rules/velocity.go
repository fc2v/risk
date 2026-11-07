package rules

import (
	"time"

	"risk/risk"
	"risk/risk/engine"
)

type Velocity struct {
	Window          time.Duration
	Multiplier      float64
	MinCount        int
	MinBaselineTxs  int
	MinBaselineSpan time.Duration
	Score           int
}

func (Velocity) SupportedKinds() []risk.TxKind { return risk.TxKinds }

// The baseline excludes the window so a burst can't inflate its own expected rate.
func (r Velocity) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	windowStart := t.PostedAt.Add(-r.Window)
	countInWindow := 1 // t itself
	for _, p := range ec.Profile.Recent {
		if p.PostedAt.After(windowStart) && !p.PostedAt.After(t.PostedAt) {
			countInWindow++
		}
	}
	baselineCount, oldestBaseline := 0, time.Time{}
	for _, h := range ec.Profile.HoursBefore(windowStart) {
		baselineCount += h.Count
		if oldestBaseline.IsZero() || h.Hour.Before(oldestBaseline) {
			oldestBaseline = h.Hour
		}
	}

	evidence := map[string]any{
		"countInWindow":  countInWindow,
		"window":         seconds(r.Window),
		"minCount":       r.MinCount,
		"multiplier":     r.Multiplier,
		"baselineTxs":    baselineCount,
		"minBaselineTxs": r.MinBaselineTxs,
	}
	outcome := risk.RuleOutcome{Reason: risk.ReasonInsufficientHistory, Score: r.Score, Evidence: evidence}

	if baselineCount < r.MinBaselineTxs {
		return outcome
	}
	baselineSpan := windowStart.Sub(oldestBaseline)
	evidence["baselineSpan"] = seconds(baselineSpan)
	if baselineSpan < r.MinBaselineSpan {
		return outcome
	}

	expectedInWindow := float64(baselineCount) * float64(r.Window) / float64(baselineSpan)
	evidence["expectedInWindow"] = expectedInWindow
	outcome.Triggered = countInWindow >= r.MinCount && float64(countInWindow) > r.Multiplier*expectedInWindow
	outcome.Reason = pick(outcome.Triggered, risk.ReasonVelocityAboveNormal, risk.ReasonVelocityWithinNormal)
	return outcome
}
