package state

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"risk/config"
	"risk/db"
	"risk/metrics"
	"risk/risk/engine"
	"risk/risk/engine/rules"
	"risk/risk/ops"
)

type backends struct {
	logger        *slog.Logger
	engine        *engine.Engine
	evaluatedTxDB db.EvaluatedTxModel
	profiles      db.ProfileModel
	profileWindow engine.ProfileWindow
	publisher     ops.Publisher
	metrics       *metrics.Metrics
}

var _ ops.Backends = (*backends)(nil)

func NewBackends(cfg config.Config, logger *slog.Logger, sqlDB *sql.DB, publisher ops.Publisher, m *metrics.Metrics) (ops.Backends, error) {
	e, err := newEngine(cfg)
	if err != nil {
		return nil, err
	}
	return &backends{
		logger:        logger,
		engine:        e,
		evaluatedTxDB: db.EvaluatedTxModel{DB: sqlDB},
		profiles:      db.ProfileModel{DB: sqlDB},
		profileWindow: profileWindow(cfg),
		publisher:     publisher,
		metrics:       m,
	}, nil
}

// profileWindow covers what the enabled rules read: the lookback, the longest raw window, and the
// most amounts any median needs.
func profileWindow(cfg config.Config) engine.ProfileWindow {
	w := engine.ProfileWindow{Lookback: cfg.History.Lookback.Std()}
	for _, ri := range cfg.Rules {
		if !ri.Enabled {
			continue
		}
		switch p := ri.Params.(type) {
		case *config.VelocityParams:
			w.RecentWindow = max(w.RecentWindow, p.Window.Std())
		case *config.PassThroughParams:
			w.RecentWindow = max(w.RecentWindow, p.Window.Std())
		case *config.HighAmountParams:
			w.LastAmounts = max(w.LastAmounts, p.MedianOfLast)
		}
	}
	return w
}

func newEngine(cfg config.Config) (*engine.Engine, error) {
	bands := engine.Bands{MinMedium: cfg.Bands.MinMedium, MinHigh: cfg.Bands.MinHigh}

	var scoped []engine.ScopedRule
	var errs []error
	for _, ri := range cfg.Rules {
		if !ri.Enabled {
			continue
		}
		s, err := engine.Scope(ri.Name, newRule(ri), ri.AppliesTo, ri.LearnsFrom)
		if err != nil {
			errs = append(errs, fmt.Errorf("rules[%s]: %w", ri.Name, err))
			continue
		}
		scoped = append(scoped, s)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return engine.New(bands, scoped...), nil
}

func newRule(ri config.RuleInstance) engine.Rule {
	switch p := ri.Params.(type) {
	case *config.VelocityParams:
		return rules.Velocity{
			Window:          p.Window.Std(),
			Multiplier:      p.Multiplier,
			MinCount:        p.MinCount,
			MinBaselineTxs:  p.MinBaselineTxs,
			MinBaselineSpan: p.MinBaselineSpan.Std(),
			Score:           ri.Score,
		}
	case *config.HighAmountParams:
		return rules.HighAmount{
			Multiplier:       p.Multiplier,
			MinBaselineTxs:   p.MinBaselineTxs,
			MedianOfLast:     p.MedianOfLast,
			DefaultThreshold: p.DefaultThreshold,
			Score:            ri.Score,
		}
	case *config.UnusualTimeParams:
		return rules.UnusualTime{
			NeighbourHours: p.NeighbourHours,
			MaxShare:       p.MaxShare,
			MinBaselineTxs: p.MinBaselineTxs,
			ExcludeRecent:  p.ExcludeRecent.Std(),
			Score:          ri.Score,
		}
	case *config.FreshPayeeParams:
		return rules.FreshPayee{MaxPayeeAge: p.MaxPayeeAge.Std(), MinAmount: p.MinAmount, Score: ri.Score}
	case *config.PayeeNameMismatchParams:
		return rules.PayeeNameMismatch{MismatchValues: p.MismatchValues, Score: ri.Score}
	case *config.NewCreditorParams:
		return rules.NewCreditor{MandateTypes: p.MandateTypes, MinBaselineTxs: p.MinBaselineTxs, Score: ri.Score}
	case *config.AboveMandateMaxParams:
		return rules.AboveMandateMax{Score: ri.Score}
	case *config.NewCountryParams:
		return rules.NewCountry{PaymentChannels: p.PaymentChannels, MinBaselineTxs: p.MinBaselineTxs, Score: ri.Score}
	case *config.NewDeviceParams:
		return rules.NewDevice{MinBaselineTxs: p.MinBaselineTxs, Score: ri.Score}
	case *config.NewCounterpartyParams:
		return rules.NewCounterparty{MinBaselineTxs: p.MinBaselineTxs, Score: ri.Score}
	case *config.RepresentedDebitParams:
		return rules.RepresentedDebit{Sequences: p.Sequences, Score: ri.Score}
	case *config.HighRiskMCCParams:
		return rules.HighRiskMCC{MCCs: p.MCCs, FirstTimeOnly: p.FirstTimeOnly, Score: ri.Score}
	case *config.HighRiskCategoryParams:
		return rules.HighRiskCategory{Categories: p.Categories, Score: ri.Score}
	case *config.PassThroughParams:
		return rules.PassThrough{
			Window:              p.Window.Std(),
			MinInbound:          p.MinInbound,
			MinShare:            p.MinShare,
			NewCounterpartyOnly: p.NewCounterpartyOnly,
			Score:               ri.Score,
		}
	default:
		panic(fmt.Sprintf("rule instance %s: no engine rule for %T", ri.Name, ri.Params))
	}
}

func (b *backends) Logger() *slog.Logger                { return b.logger }
func (b *backends) Engine() *engine.Engine              { return b.engine }
func (b *backends) EvaluatedTxDB() ops.EvaluatedTxDB    { return b.evaluatedTxDB }
func (b *backends) Publisher() ops.Publisher            { return b.publisher }
func (b *backends) Profiles() ops.Profiles              { return b.profiles }
func (b *backends) ProfileWindow() engine.ProfileWindow { return b.profileWindow }
func (b *backends) Metrics() ops.Metrics                { return b.metrics }
