package rules

import (
	"risk/risk"
	"risk/risk/engine"
)

type AboveMandateMax struct {
	Score int
}

func (AboveMandateMax) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindDebitOrder} }

func (r AboveMandateMax) Evaluate(t risk.Tx, _ engine.EvaluationContext) risk.RuleOutcome {
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: map[string]any{}}

	switch {
	case t.DebitOrder == nil:
		outcome.Reason = risk.ReasonNotApplicable
	case t.MandateMaxAmount == nil:
		outcome.Reason = risk.ReasonMandateMaxUnknown
	default:
		outcome.Evidence["mandateMaxAmount"] = *t.MandateMaxAmount
		outcome.Triggered = size(t.Amount) > *t.MandateMaxAmount
		outcome.Reason = pick(outcome.Triggered, risk.ReasonAboveMandateMax, risk.ReasonWithinMandateMax)
	}
	return outcome
}
