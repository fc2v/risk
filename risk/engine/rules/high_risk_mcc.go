package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type HighRiskMCC struct {
	MCCs          []string
	FirstTimeOnly bool
	Score         int
}

func (HighRiskMCC) SupportedKinds() []risk.TxKind {
	return []risk.TxKind{risk.KindCard, risk.KindCardRefund}
}

func (r HighRiskMCC) Evaluate(t risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	evidence := map[string]any{"mccs": r.MCCs}
	outcome := risk.RuleOutcome{Reason: risk.ReasonNotApplicable, Score: r.Score, Evidence: evidence}

	if t.Card == nil {
		return outcome
	}
	evidence["mcc"] = t.MCC
	if !slices.Contains(r.MCCs, t.MCC) {
		outcome.Reason = risk.ReasonMCCNotHighRisk
		return outcome
	}

	if r.FirstTimeOnly {
		prior := 0
		for _, mcc := range r.MCCs {
			if ec.Profile.HasSeen(risk.FieldMCC, mcc) {
				prior++
			}
		}
		evidence["priorHighRiskMCCs"] = prior
		outcome.Triggered = prior == 0
		outcome.Reason = pick(outcome.Triggered, risk.ReasonHighRiskMCC, risk.ReasonHighRiskMCCSeenBefore)
		return outcome
	}

	outcome.Triggered, outcome.Reason = true, risk.ReasonHighRiskMCC
	return outcome
}
