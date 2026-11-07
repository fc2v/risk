package engine

import (
	"fmt"
	"slices"

	"risk/risk"
)

type EvaluationContext struct {
	Profile Profile
}

const MaxScore = 100

type Bands struct {
	MinMedium int
	MinHigh   int
}

func (b Bands) Classify(score int) risk.Risk {
	switch {
	case score >= b.MinHigh:
		return risk.RiskHigh
	case score >= b.MinMedium:
		return risk.RiskMedium
	default:
		return risk.RiskLow
	}
}

type Result struct {
	Score    int
	Risk     risk.Risk
	Outcomes []risk.RuleOutcome
}

type Rule interface {
	Evaluate(t risk.Tx, ec EvaluationContext) risk.RuleOutcome
	SupportedKinds() []risk.TxKind
}

type ScopedRule struct {
	name       string
	rule       Rule
	appliesTo  []risk.TxKind
	learnsFrom []risk.TxKind
}

// SupportedKinds limits only appliesTo: rules handle any kind in their history, so learnsFrom is
// a tuning choice left to config. learnsFrom defaults to appliesTo.
func Scope(name string, r Rule, appliesTo, learnsFrom []risk.TxKind) (ScopedRule, error) {
	if len(learnsFrom) == 0 {
		learnsFrom = appliesTo
	}
	for _, k := range appliesTo {
		if !slices.Contains(r.SupportedKinds(), k) {
			return ScopedRule{}, fmt.Errorf("%T does not support %s txs", r, k)
		}
	}
	return ScopedRule{name: name, rule: r, appliesTo: appliesTo, learnsFrom: learnsFrom}, nil
}

func (s ScopedRule) appliesToTx(t risk.Tx) bool {
	return slices.Contains(s.appliesTo, t.Kind())
}

type Engine struct {
	rules []ScopedRule
	bands Bands
}

func New(bands Bands, rules ...ScopedRule) *Engine {
	return &Engine{rules: rules, bands: bands}
}

func (e *Engine) Evaluate(t risk.Tx, ec EvaluationContext) Result {
	res := Result{Outcomes: []risk.RuleOutcome{}}
	for _, r := range e.rules {
		if !r.appliesToTx(t) {
			continue
		}
		o := r.rule.Evaluate(t, EvaluationContext{Profile: ec.Profile.OfKinds(r.learnsFrom)})
		o.Rule = r.name
		if o.Triggered {
			res.Score += o.Score
		}
		res.Outcomes = append(res.Outcomes, o)
	}

	if len(res.Outcomes) == 0 {
		res.Risk = risk.RiskUnknown
		return res
	}
	res.Score = min(res.Score, MaxScore)
	res.Risk = e.bands.Classify(res.Score)
	return res
}
