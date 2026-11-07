package engine_test

import (
	"slices"
	"testing"
	"time"

	"risk/risk"
	"risk/risk/engine"
	"risk/risk/engine/rules"
)

type cardOnlyRule struct{ seen *[]risk.TxKind }

func (cardOnlyRule) SupportedKinds() []risk.TxKind { return []risk.TxKind{risk.KindCard} }

func (r cardOnlyRule) Evaluate(_ risk.Tx, ec engine.EvaluationContext) risk.RuleOutcome {
	for _, h := range ec.Profile.Hours {
		*r.seen = append(*r.seen, h.Kind)
	}
	return risk.RuleOutcome{Reason: risk.ReasonNotApplicable}
}

func kinds(k ...risk.TxKind) []risk.TxKind { return k }

func TestScopeLimitsOnlyAppliesTo(t *testing.T) {
	cases := []struct {
		name       string
		rule       engine.Rule
		appliesTo  []risk.TxKind
		learnsFrom []risk.TxKind
		wantErr    bool
	}{
		{"passThrough can't judge incoming money", rules.PassThrough{}, kinds(risk.KindTransferIn), nil, true},
		{"passThrough can't judge own-account moves", rules.PassThrough{}, kinds(risk.KindOwnAccountTransfer), nil, true},
		{"passThrough learns from inflows", rules.PassThrough{},
			kinds(risk.KindTransferOut), kinds(risk.KindTransferIn, risk.KindOwnAccountTransfer, risk.KindTransferOut), false},
		{"any rule may learn from any kind", cardOnlyRule{}, kinds(risk.KindCard), kinds(risk.KindDebitOrder), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := engine.Scope("r", tc.rule, tc.appliesTo, tc.learnsFrom)
			if (err != nil) != tc.wantErr {
				t.Errorf("Scope() error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestBaselineFollowsLearnsFrom(t *testing.T) {
	now := time.Now()
	card := risk.Tx{Type: risk.TxTypeCard, Amount: -100, PostedAt: now, Card: &risk.Card{}}
	debit := risk.Tx{Type: risk.TxTypeDebitOrder, Amount: -100, PostedAt: now, DebitOrder: &risk.DebitOrder{}}
	history := engine.EvaluationContext{Profile: engine.BuildProfile([]risk.Tx{card, debit},
		engine.ProfileWindow{At: now, Lookback: time.Hour, RecentWindow: time.Hour, LastAmounts: 1})}

	for name, tc := range map[string]struct {
		learnsFrom []risk.TxKind
		want       []risk.TxKind
	}{
		"defaults to appliesTo": {nil, kinds(risk.KindCard)},
		"configured kinds only": {kinds(risk.KindDebitOrder), kinds(risk.KindDebitOrder)},
	} {
		t.Run(name, func(t *testing.T) {
			var seen []risk.TxKind
			scoped, err := engine.Scope("r", cardOnlyRule{seen: &seen}, kinds(risk.KindCard), tc.learnsFrom)
			if err != nil {
				t.Fatal(err)
			}
			engine.New(engine.Bands{MinMedium: 30, MinHigh: 60}, scoped).Evaluate(card, history)
			if !slices.Equal(seen, tc.want) {
				t.Errorf("baseline kinds = %v, want %v", seen, tc.want)
			}
		})
	}
}
