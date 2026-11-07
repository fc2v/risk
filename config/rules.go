package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"risk/risk"
)

type RuleInstance struct {
	Name       string        `json:"name"`
	Rule       string        `json:"rule"`
	Enabled    bool          `json:"enabled"`
	Score      int           `json:"score"`
	AppliesTo  []risk.TxKind `json:"appliesTo"`
	LearnsFrom []risk.TxKind `json:"learnsFrom"`
	Params     RuleParams    `json:"-"`
}

type RuleParams interface {
	validate(lookback Duration) []error
}

var ruleParams = map[string]func() RuleParams{
	"velocity":          func() RuleParams { return &VelocityParams{} },
	"highAmount":        func() RuleParams { return &HighAmountParams{} },
	"unusualTime":       func() RuleParams { return &UnusualTimeParams{} },
	"freshPayee":        func() RuleParams { return &FreshPayeeParams{} },
	"payeeNameMismatch": func() RuleParams { return &PayeeNameMismatchParams{} },
	"newCreditor":       func() RuleParams { return &NewCreditorParams{} },
	"aboveMandateMax":   func() RuleParams { return &AboveMandateMaxParams{} },
	"newCountry":        func() RuleParams { return &NewCountryParams{} },
	"newDevice":         func() RuleParams { return &NewDeviceParams{} },
	"newCounterparty":   func() RuleParams { return &NewCounterpartyParams{} },
	"representedDebit":  func() RuleParams { return &RepresentedDebitParams{} },
	"passThrough":       func() RuleParams { return &PassThroughParams{} },
	"highRiskMCC":       func() RuleParams { return &HighRiskMCCParams{} },
	"highRiskCategory":  func() RuleParams { return &HighRiskCategoryParams{} },
}

var instanceFields = []string{"name", "rule", "enabled", "score", "appliesTo", "learnsFrom"}

// Fields other than the instance's own must belong to the named rule's params.
func (ri *RuleInstance) UnmarshalJSON(b []byte) error {
	type instance RuleInstance
	if err := json.Unmarshal(b, (*instance)(ri)); err != nil {
		return err
	}

	newParams, ok := ruleParams[ri.Rule]
	if !ok {
		return fmt.Errorf("rule instance %q: unknown rule %q, must be one of %v", ri.Name, ri.Rule, ruleNames())
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for _, f := range instanceFields {
		delete(fields, f)
	}
	paramsJSON, err := json.Marshal(fields)
	if err != nil {
		return err
	}

	ri.Params = newParams()
	dec := json.NewDecoder(bytes.NewReader(paramsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(ri.Params); err != nil {
		return fmt.Errorf("rule instance %q (%s): %w", ri.Name, ri.Rule, err)
	}
	return nil
}

func ruleNames() []string {
	names := make([]string, 0, len(ruleParams))
	for n := range ruleParams {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func validateRules(instances []RuleInstance, lookback Duration) []error {
	var errs []error
	seen := map[string]bool{}

	for i, ri := range instances {
		field := fmt.Sprintf("rules[%d]", i)
		if ri.Name != "" {
			field = fmt.Sprintf("rules[%s]", ri.Name)
		}

		switch {
		case ri.Name == "":
			errs = append(errs, fmt.Errorf("%s.name must be set", field))
		case seen[ri.Name]:
			errs = append(errs, fmt.Errorf("%s.name must be unique", field))
		}
		seen[ri.Name] = true

		if !ri.Enabled {
			continue
		}

		if ri.Score < 0 || ri.Score > 100 {
			errs = append(errs, fmt.Errorf("%s.score must be between 0 and 100", field))
		}
		errs = append(errs, checkAppliesTo(field+".appliesTo", ri.AppliesTo))
		if len(ri.LearnsFrom) > 0 {
			errs = append(errs, checkAppliesTo(field+".learnsFrom", ri.LearnsFrom))
		}
		for _, err := range ri.Params.validate(lookback) {
			errs = append(errs, fmt.Errorf("%s.%w", field, err))
		}
	}
	return errs
}

func checkAppliesTo(field string, kinds []risk.TxKind) error {
	if len(kinds) == 0 {
		return fmt.Errorf("%s must list at least one of %v", field, risk.TxKinds)
	}
	for _, k := range kinds {
		if !slices.Contains(risk.TxKinds, k) {
			return fmt.Errorf("%s: unknown kind %q, must be one of %v", field, k, risk.TxKinds)
		}
	}
	return nil
}

type requirements []error

func (r *requirements) require(ok bool, format string, args ...any) {
	if !ok {
		*r = append(*r, fmt.Errorf(format, args...))
	}
}

type VelocityParams struct {
	Window          Duration `json:"window"`
	Multiplier      float64  `json:"multiplier"`
	MinCount        int      `json:"minCount"`
	MinBaselineTxs  int      `json:"minBaselineTxs"`
	MinBaselineSpan Duration `json:"minBaselineSpan"`
}

func (p *VelocityParams) validate(lookback Duration) []error {
	var r requirements
	r.require(p.Window > 0, "window must be > 0")
	r.require(p.Multiplier > 0, "multiplier must be > 0")
	r.require(p.MinCount >= 1, "minCount must be >= 1")
	r.require(p.MinBaselineTxs >= 1, "minBaselineTxs must be >= 1")
	r.require(p.MinBaselineSpan > 0, "minBaselineSpan must be > 0")
	needed := p.Window + p.MinBaselineSpan
	r.require(lookback >= needed, "window + minBaselineSpan (%s) must fit within history.lookback (%s)", needed.Std(), lookback.Std())
	return r
}

type HighAmountParams struct {
	Multiplier       float64 `json:"multiplier"`
	MinBaselineTxs   int     `json:"minBaselineTxs"`
	MedianOfLast     int     `json:"medianOfLast"`
	DefaultThreshold int64   `json:"defaultThreshold"`
}

func (p *HighAmountParams) validate(Duration) []error {
	var r requirements
	r.require(p.Multiplier > 0, "multiplier must be > 0")
	r.require(p.MinBaselineTxs >= 1, "minBaselineTxs must be >= 1")
	r.require(p.MedianOfLast >= 1 && p.MedianOfLast >= p.MinBaselineTxs, "medianOfLast must be >= 1 and >= minBaselineTxs")
	r.require(p.DefaultThreshold > 0, "defaultThreshold must be > 0")
	return r
}

type UnusualTimeParams struct {
	NeighbourHours int      `json:"neighbourHours"`
	MaxShare       float64  `json:"maxShare"`
	MinBaselineTxs int      `json:"minBaselineTxs"`
	ExcludeRecent  Duration `json:"excludeRecent"`
}

func (p *UnusualTimeParams) validate(lookback Duration) []error {
	var r requirements
	r.require(p.NeighbourHours >= 0 && p.NeighbourHours <= 11, "neighbourHours must be between 0 and 11")
	r.require(p.MaxShare > 0 && p.MaxShare < 1, "maxShare must be between 0 and 1")
	r.require(p.MinBaselineTxs >= 1, "minBaselineTxs must be >= 1")
	r.require(p.ExcludeRecent >= 0, "excludeRecent must be >= 0")
	r.require(lookback > p.ExcludeRecent, "excludeRecent must be shorter than history.lookback")
	return r
}

type FreshPayeeParams struct {
	MaxPayeeAge Duration `json:"maxPayeeAge"`
	MinAmount   int64    `json:"minAmount"`
}

func (p *FreshPayeeParams) validate(Duration) []error {
	var r requirements
	r.require(p.MaxPayeeAge > 0, "maxPayeeAge must be > 0")
	r.require(p.MinAmount >= 0, "minAmount must be >= 0")
	return r
}

type PayeeNameMismatchParams struct {
	MismatchValues []string `json:"mismatchValues"`
}

func (p *PayeeNameMismatchParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.MismatchValues) > 0, "mismatchValues must list at least one value")
	return r
}

type NewCreditorParams struct {
	MandateTypes   []string `json:"mandateTypes"`
	MinBaselineTxs int      `json:"minBaselineTxs"`
}

func (p *NewCreditorParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.MandateTypes) > 0, "mandateTypes must list at least one value")
	r.require(p.MinBaselineTxs >= 0, "minBaselineTxs must be >= 0")
	return r
}

type AboveMandateMaxParams struct{}

func (p *AboveMandateMaxParams) validate(Duration) []error { return nil }

type NewCountryParams struct {
	PaymentChannels []string `json:"paymentChannels"`
	MinBaselineTxs  int      `json:"minBaselineTxs"`
}

func (p *NewCountryParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.PaymentChannels) > 0, "paymentChannels must list at least one value")
	r.require(p.MinBaselineTxs >= 0, "minBaselineTxs must be >= 0")
	return r
}

type NewDeviceParams struct {
	MinBaselineTxs int `json:"minBaselineTxs"`
}

func (p *NewDeviceParams) validate(Duration) []error {
	var r requirements
	r.require(p.MinBaselineTxs >= 0, "minBaselineTxs must be >= 0")
	return r
}

type NewCounterpartyParams struct {
	MinBaselineTxs int `json:"minBaselineTxs"`
}

func (p *NewCounterpartyParams) validate(Duration) []error {
	var r requirements
	r.require(p.MinBaselineTxs >= 0, "minBaselineTxs must be >= 0")
	return r
}

type RepresentedDebitParams struct {
	Sequences []string `json:"sequences"`
}

func (p *RepresentedDebitParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.Sequences) > 0, "sequences must list at least one value")
	return r
}

type HighRiskMCCParams struct {
	MCCs          []string `json:"mccs"`
	FirstTimeOnly bool     `json:"firstTimeOnly"`
}

func (p *HighRiskMCCParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.MCCs) > 0, "mccs must list at least one value")
	for _, mcc := range p.MCCs {
		r.require(isMCC(mcc), "mccs: %q must be 4 digits", mcc)
	}
	return r
}

type HighRiskCategoryParams struct {
	Categories []string `json:"categories"`
}

func (p *HighRiskCategoryParams) validate(Duration) []error {
	var r requirements
	r.require(len(p.Categories) > 0, "categories must list at least one value")
	r.require(!slices.Contains(p.Categories, ""), "categories must not contain an empty value")
	return r
}

func isMCC(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type PassThroughParams struct {
	Window              Duration `json:"window"`
	MinInbound          int64    `json:"minInbound"`
	MinShare            float64  `json:"minShare"`
	NewCounterpartyOnly bool     `json:"newCounterpartyOnly"`
}

func (p *PassThroughParams) validate(lookback Duration) []error {
	var r requirements
	r.require(p.Window > 0, "window must be > 0")
	r.require(lookback >= p.Window, "window must fit within history.lookback")
	r.require(p.MinInbound > 0, "minInbound must be > 0")
	r.require(p.MinShare > 0 && p.MinShare <= 1, "minShare must be > 0 and <= 1")
	return r
}
