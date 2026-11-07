package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type NewCreditor struct {
	MandateTypes   []string
	MinBaselineTxs int
	Score          int
}

func (NewCreditor) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindDebitOrder} }

func (r NewCreditor) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	baselineTxs := ec.Profile.TxCount()
	evidence := map[string]any{
		"baselineTxs":    baselineTxs,
		"minBaselineTxs": r.MinBaselineTxs,
		"priorCreditors": ec.Profile.Distinct(risk.FieldCreditor),
		"mandateTypes":   r.MandateTypes,
	}
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: evidence}

	switch {
	case t.DebitOrder == nil:
		outcome.Reason = risk.ReasonNotApplicable
		return outcome
	case baselineTxs < r.MinBaselineTxs:
		outcome.Reason = risk.ReasonInsufficientHistory
		return outcome
	}

	evidence["creditorUserCode"] = t.CreditorUserCode
	evidence["mandateType"] = t.MandateType
	evidence["sequence"] = t.Sequence
	switch {
	case !slices.Contains(r.MandateTypes, t.MandateType):
		outcome.Reason = risk.ReasonMandateTypeNotChecked
	case ec.Profile.HasSeen(risk.FieldCreditor, t.CreditorUserCode):
		outcome.Reason = risk.ReasonKnownCreditor
	default:
		outcome.Triggered, outcome.Reason = true, risk.ReasonNewCreditor
	}
	return outcome
}
