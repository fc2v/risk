package engine

import (
	"cmp"
	"slices"
	"time"

	"risk/risk"
)

// Profile is what rules know about an account's history at the moment a tx is evaluated.
type Profile struct {
	Hours   []HourSummary
	Seen    []SeenValue
	Amounts []RecentAmount
	Recent  []RecentTx
}

type HourSummary struct {
	Kind       risk.TxKind `json:"kind"`
	Hour       time.Time   `json:"hour"`
	Count      int         `json:"count"`
	Inflow     int64       `json:"inflow"`
	Outflow    int64       `json:"outflow"`
	WithDevice int         `json:"withDevice"`
}

type SeenValue struct {
	Field     risk.Field  `json:"field"`
	Value     string      `json:"value"`
	Kind      risk.TxKind `json:"kind"`
	FirstSeen time.Time   `json:"firstSeen"`
	LastSeen  time.Time   `json:"lastSeen"`
}

type RecentAmount struct {
	Kind     risk.TxKind `json:"kind"`
	PostedAt time.Time   `json:"postedAt"`
	Size     int64       `json:"size"`
}

type RecentTx struct {
	Kind         risk.TxKind `json:"kind"`
	PostedAt     time.Time   `json:"postedAt"`
	Amount       int64       `json:"amount"`
	Counterparty string      `json:"counterparty"`
}

// ProfileWindow is what a Profile covers for a tx posted at At.
type ProfileWindow struct {
	At           time.Time
	Lookback     time.Duration
	RecentWindow time.Duration
	LastAmounts  int
}

func (w ProfileWindow) HourRange() (first, last time.Time) {
	return w.At.Add(-w.Lookback).UTC().Truncate(time.Hour), w.At.UTC().Truncate(time.Hour)
}

// BuildProfile is the reference for what db.ProfileModel.Load returns, built from an account's txs.
func BuildProfile(txs []risk.Tx, w ProfileWindow) Profile {
	firstHour, lastHour := w.HourRange()
	lookbackStart := w.At.Add(-w.Lookback)
	recentStart := w.At.Add(-w.RecentWindow)

	var p Profile
	hours := map[[2]any]*HourSummary{}
	seen := map[[3]any]*SeenValue{}
	amounts := map[risk.TxKind][]RecentAmount{}

	for _, t := range txs {
		kind := t.Kind()
		t.PostedAt = t.PostedAt.UTC()
		if h := t.PostedAt.UTC().Truncate(time.Hour); !h.Before(firstHour) && !h.After(lastHour) {
			key := [2]any{kind, h}
			if hours[key] == nil {
				hours[key] = &HourSummary{Kind: kind, Hour: h}
			}
			s := hours[key]
			s.Count++
			s.Inflow += max(t.Amount, 0)
			s.Outflow += max(-t.Amount, 0)
			if _, ok := t.Fields()[risk.FieldDevice]; ok {
				s.WithDevice++
			}
		}

		for f, v := range t.Fields() {
			key := [3]any{f, v, kind}
			if seen[key] == nil {
				seen[key] = &SeenValue{Field: f, Value: v, Kind: kind, FirstSeen: t.PostedAt, LastSeen: t.PostedAt}
			}
			s := seen[key]
			if t.PostedAt.Before(s.FirstSeen) {
				s.FirstSeen = t.PostedAt
			}
			if t.PostedAt.After(s.LastSeen) {
				s.LastSeen = t.PostedAt
			}
		}

		within := t.PostedAt.After(lookbackStart) && !t.PostedAt.After(w.At)
		if within {
			amounts[kind] = append(amounts[kind], RecentAmount{Kind: kind, PostedAt: t.PostedAt, Size: max(t.Amount, -t.Amount)})
		}
		if t.PostedAt.After(recentStart) && !t.PostedAt.After(w.At) {
			p.Recent = append(p.Recent, RecentTx{Kind: kind, PostedAt: t.PostedAt, Amount: t.Amount, Counterparty: t.Fields()[risk.FieldCounterparty]})
		}
	}

	for _, s := range hours {
		p.Hours = append(p.Hours, *s)
	}
	for _, s := range seen {
		if s.LastSeen.After(lookbackStart) && s.FirstSeen.Before(w.At) {
			p.Seen = append(p.Seen, *s)
		}
	}
	for _, a := range amounts {
		slices.SortFunc(a, func(x, y RecentAmount) int { return y.PostedAt.Compare(x.PostedAt) })
		p.Amounts = append(p.Amounts, a[:min(len(a), w.LastAmounts)]...)
	}
	p.Sort()
	return p
}

// UTC converts every time to UTC, so profiles from different sources compare equal.
func (p *Profile) UTC() {
	for i := range p.Hours {
		p.Hours[i].Hour = p.Hours[i].Hour.UTC()
	}
	for i := range p.Seen {
		p.Seen[i].FirstSeen, p.Seen[i].LastSeen = p.Seen[i].FirstSeen.UTC(), p.Seen[i].LastSeen.UTC()
	}
	for i := range p.Amounts {
		p.Amounts[i].PostedAt = p.Amounts[i].PostedAt.UTC()
	}
	for i := range p.Recent {
		p.Recent[i].PostedAt = p.Recent[i].PostedAt.UTC()
	}
}

// Sort puts every slice in a fixed order, so profiles from different sources compare equal.
func (p *Profile) Sort() {
	slices.SortFunc(p.Hours, func(a, b HourSummary) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), a.Hour.Compare(b.Hour))
	})
	slices.SortFunc(p.Seen, func(a, b SeenValue) int {
		return cmp.Or(cmp.Compare(a.Field, b.Field), cmp.Compare(a.Value, b.Value), cmp.Compare(a.Kind, b.Kind))
	})
	slices.SortFunc(p.Amounts, func(a, b RecentAmount) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), b.PostedAt.Compare(a.PostedAt))
	})
	slices.SortFunc(p.Recent, func(a, b RecentTx) int {
		return cmp.Or(a.PostedAt.Compare(b.PostedAt), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Amount, b.Amount))
	})
}

func (p Profile) OfKinds(kinds []risk.TxKind) Profile {
	keep := func(k risk.TxKind) bool { return slices.Contains(kinds, k) }
	var out Profile
	for _, h := range p.Hours {
		if keep(h.Kind) {
			out.Hours = append(out.Hours, h)
		}
	}
	for _, s := range p.Seen {
		if keep(s.Kind) {
			out.Seen = append(out.Seen, s)
		}
	}
	for _, a := range p.Amounts {
		if keep(a.Kind) {
			out.Amounts = append(out.Amounts, a)
		}
	}
	for _, r := range p.Recent {
		if keep(r.Kind) {
			out.Recent = append(out.Recent, r)
		}
	}
	return out
}

func (p Profile) TxCount() int {
	n := 0
	for _, h := range p.Hours {
		n += h.Count
	}
	return n
}

func (p Profile) WithDevice() int {
	n := 0
	for _, h := range p.Hours {
		n += h.WithDevice
	}
	return n
}

// HoursBefore are the hours that ended by cut, so baselines end on a whole hour.
func (p Profile) HoursBefore(cut time.Time) []HourSummary {
	var out []HourSummary
	for _, h := range p.Hours {
		if !h.Hour.Add(time.Hour).After(cut) {
			out = append(out, h)
		}
	}
	return out
}

func (p Profile) Distinct(f risk.Field) int {
	values := map[string]bool{}
	for _, s := range p.Seen {
		if s.Field == f {
			values[s.Value] = true
		}
	}
	return len(values)
}

func (p Profile) HasSeen(f risk.Field, value string) bool {
	return slices.ContainsFunc(p.Seen, func(s SeenValue) bool { return s.Field == f && s.Value == value })
}
