package rules

import (
	"time"

	"risk/risk"
	"risk/risk/engine"
)

type FreshPayee struct {
	MaxPayeeAge time.Duration
	MinAmount   int64
	Score       int
}

func (FreshPayee) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindTransferOut} }

func (r FreshPayee) Evaluate(t risk.Tx, _ engine.EvaluationContext) risk.RuleOutcome {
	evidence := map[string]any{
		"maxPayeeAge": seconds(r.MaxPayeeAge),
		"minAmount":   r.MinAmount,
	}
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: evidence}

	switch {
	case t.Transfer == nil:
		outcome.Reason = risk.ReasonNotApplicable
		return outcome
	case t.PayeeAddedAt == nil:
		outcome.Reason = risk.ReasonPayeeAgeUnknown
		return outcome
	}

	payeeAge := t.PostedAt.Sub(*t.PayeeAddedAt)
	evidence["payeeAge"] = seconds(payeeAge)
	switch {
	case payeeAge > r.MaxPayeeAge:
		outcome.Reason = risk.ReasonPayeeNotFresh
	case size(t.Amount) < r.MinAmount:
		outcome.Reason = risk.ReasonAmountBelowMinimum
	default:
		outcome.Triggered, outcome.Reason = true, risk.ReasonFreshPayee
	}
	return outcome
}
