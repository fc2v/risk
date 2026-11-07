package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type NewCountry struct {
	PaymentChannels []string
	MinBaselineTxs  int
	Score           int
}

func (NewCountry) SupportedKinds() []risk.TxKind {
	return []risk.TxKind{risk.KindCard, risk.KindCardRefund}
}

func (r NewCountry) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	baselineTxs := ec.Profile.TxCount()
	evidence := map[string]any{
		"baselineTxs":     baselineTxs,
		"minBaselineTxs":  r.MinBaselineTxs,
		"priorCountries":  ec.Profile.Distinct(risk.FieldMerchantCountry),
		"paymentChannels": r.PaymentChannels,
	}
	outcome := risk.RuleOutcome{Score: r.Score, Evidence: evidence}

	switch {
	case t.Card == nil:
		outcome.Reason = risk.ReasonNotApplicable
		return outcome
	case baselineTxs < r.MinBaselineTxs:
		outcome.Reason = risk.ReasonInsufficientHistory
		return outcome
	}

	evidence["merchantCountry"] = t.MerchantCountry
	evidence["paymentChannel"] = t.PaymentChannel
	switch {
	case !slices.Contains(r.PaymentChannels, t.PaymentChannel):
		outcome.Reason = risk.ReasonChannelNotChecked
	case ec.Profile.HasSeen(risk.FieldMerchantCountry, t.MerchantCountry):
		outcome.Reason = risk.ReasonKnownCountry
	default:
		outcome.Triggered, outcome.Reason = true, risk.ReasonNewCountry
	}
	return outcome
}
