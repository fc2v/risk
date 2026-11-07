package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type HighRiskCategory struct {
	Categories []string
	Score      int
}

func (HighRiskCategory) SupportedKinds() []risk.TxKind { return risk.TxKinds }

func (r HighRiskCategory) Evaluate(t risk.Tx, _ engine.EvaluationContext) risk.RuleOutcome {
	evidence := map[string]any{"categories": r.Categories}
	outcome := risk.RuleOutcome{Reason: risk.ReasonNotApplicable, Score: r.Score, Evidence: evidence}

	if t.Category == "" {
		return outcome
	}
	evidence["category"] = t.Category
	outcome.Triggered = slices.Contains(r.Categories, t.Category)
	outcome.Reason = pick(outcome.Triggered, risk.ReasonHighRiskCategory, risk.ReasonCategoryNotHighRisk)
	return outcome
}
