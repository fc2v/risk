package rules

import (
	"time"

	"risk/risk"
	"risk/risk/engine"
)

type PassThrough struct {
	Window              time.Duration
	MinInbound          int64
	MinShare            float64
	NewCounterpartyOnly bool
	Score               int
}

func (PassThrough) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindTransferOut} }

// Outflows are summed so a mule splitting the money across several payees still triggers,
// on the payment that takes the total past MinShare.
func (r PassThrough) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	windowStart := t.PostedAt.Add(-r.Window)

	var inbound, outbound int64
	var firstInbound time.Time
	for _, p := range ec.Profile.Recent {
		inWindow := p.PostedAt.After(windowStart) && !p.PostedAt.After(t.PostedAt)
		if inWindow && p.Amount > 0 {
			inbound += p.Amount
			if firstInbound.IsZero() || p.PostedAt.Before(firstInbound) {
				firstInbound = p.PostedAt
			}
		}
	}
	paidBefore := t.Transfer != nil &&
		ec.Profile.OfKinds([]risk.TxKind{risk.KindTransferOut}).HasSeen(risk.FieldCounterparty, t.CounterpartyAccountHash)

	outbound = size(t.Amount)
	for _, p := range ec.Profile.Recent {
		if p.Kind == risk.KindTransferOut && !firstInbound.IsZero() &&
			!p.PostedAt.Before(firstInbound) && !p.PostedAt.After(t.PostedAt) {
			outbound += size(p.Amount)
		}
	}

	evidence := map[string]any{
		"window":              seconds(r.Window),
		"inboundInWindow":     inbound,
		"minInbound":          r.MinInbound,
		"outboundSinceInflow": outbound,
		"minShare":            r.MinShare,
		"paidBefore":          paidBefore,
		"newCounterpartyOnly": r.NewCounterpartyOnly,
	}
	outcome := risk.RuleOutcome{Reason: risk.ReasonInflowBelowMinimum, Score: r.Score, Evidence: evidence}

	if inbound < r.MinInbound {
		return outcome
	}
	share := float64(outbound) / float64(inbound)
	evidence["share"] = share
	switch {
	case share < r.MinShare:
		outcome.Reason = risk.ReasonShareBelowMinimum
	case r.NewCounterpartyOnly && paidBefore:
		outcome.Reason = risk.ReasonPayeePaidBefore
	default:
		outcome.Triggered, outcome.Reason = true, risk.ReasonPassThrough
	}
	return outcome
}
