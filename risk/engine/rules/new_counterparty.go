package rules

import (
	"risk/risk"
	"risk/risk/engine"
)

type NewCounterparty struct {
	MinBaselineTxs int
	Score          int
}

func (NewCounterparty) SupportedKinds() []risk.TxKind {
	return []risk.TxKind{risk.KindTransferOut, risk.KindTransferIn}
}

func (r NewCounterparty) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	baselineTxs := ec.Profile.TxCount()
	evidence := map[string]any{
		"baselineTxs":         baselineTxs,
		"minBaselineTxs":      r.MinBaselineTxs,
		"priorCounterparties": ec.Profile.Distinct(risk.FieldCounterparty),
	}
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: evidence}

	switch {
	case t.Transfer == nil:
		outcome.Reason = risk.ReasonNotApplicable
	case baselineTxs < r.MinBaselineTxs:
		outcome.Reason = risk.ReasonInsufficientHistory
	default:
		outcome.Triggered = !ec.Profile.HasSeen(risk.FieldCounterparty, t.CounterpartyAccountHash)
		outcome.Reason = pick(outcome.Triggered, risk.ReasonNewCounterparty, risk.ReasonKnownCounterparty)
	}
	return outcome
}
