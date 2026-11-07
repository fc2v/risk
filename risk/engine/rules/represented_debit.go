package rules

import (
	"slices"

	"risk/risk"
	"risk/risk/engine"
)

type RepresentedDebit struct {
	Sequences []string
	Score     int
}

func (RepresentedDebit) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindDebitOrder} }

func (r RepresentedDebit) Evaluate(t risk.Tx, _ engine.EvaluationContext) risk.RuleOutcome {
	outcome := risk.RuleOutcome{Reason: risk.ReasonNotApplicable, Score: r.Score, Evidence: map[string]any{"sequences": r.Sequences}}
	if t.DebitOrder == nil {
		return outcome
	}

	outcome.Evidence["sequence"] = t.Sequence
	outcome.Triggered = slices.Contains(r.Sequences, t.Sequence)
	outcome.Reason = pick(outcome.Triggered, risk.ReasonRepresentedDebit, risk.ReasonNotRepresented)
	return outcome
}
