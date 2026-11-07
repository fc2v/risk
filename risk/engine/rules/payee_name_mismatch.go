package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type PayeeNameMismatch struct {
	MismatchValues []string
	Score          int
}

func (PayeeNameMismatch) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindTransferOut} }

func (r PayeeNameMismatch) Evaluate(t risk.Tx, _ engine.EvaluationContext) risk.RuleOutcome {
	outcome := risk.RuleOutcome{Reason: risk.ReasonNotApplicable, Score: r.Score, Evidence: map[string]any{"mismatchValues": r.MismatchValues}}
	if t.Transfer == nil {
		return outcome
	}

	outcome.Evidence["payeeMatch"] = t.PayeeMatch
	outcome.Triggered = slices.Contains(r.MismatchValues, t.PayeeMatch)
	outcome.Reason = pick(outcome.Triggered, risk.ReasonPayeeNameMismatch, risk.ReasonPayeeNameNotFlagged)
	return outcome
}
