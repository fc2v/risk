package engine_test

import (
	"slices"
	"testing"
	"time"

	"risk/risk"
	"risk/risk/engine"
)

func TestBuildProfile(t *testing.T) {
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	card := func(id string, amount int64, posted time.Time, mcc string) risk.Tx {
		return risk.Tx{ID: id, AccountID: "a", Type: risk.TxTypeCard, Amount: amount, PostedAt: posted,
			Card: &risk.Card{MCC: mcc, MerchantCountry: "ZA"}}
	}
	txs := []risk.Tx{
		card("before-lookback", -100, at(9, 10), "5411"),
		card("a1", -200, at(10, 0), "5411"),
		card("a2", -300, at(11, 0), "5411"),
		card("a3", -400, at(12, 0), "5411"),
		card("refund", 500, at(12, 15), "5411"),
		card("later", -600, at(13, 0), "7995"),
	}
	p := engine.BuildProfile(txs, engine.ProfileWindow{At: at(12, 30), Lookback: 3 * time.Hour, RecentWindow: time.Hour, LastAmounts: 2})

	var hours []string
	for _, h := range p.Hours {
		hours = append(hours, string(h.Kind)+" "+h.Hour.Format("15:04"))
	}
	wantHours := []string{"card 09:00", "card 10:00", "card 11:00", "card 12:00", "cardRefund 12:00"}
	if !slices.Equal(hours, wantHours) {
		t.Errorf("hours %v, want %v: from the hour holding the lookback start to the hour holding the tx", hours, wantHours)
	}

	var seen []string
	for _, s := range p.Seen {
		seen = append(seen, string(s.Field)+" "+s.Value+" "+string(s.Kind))
	}
	wantSeen := []string{"mcc 5411 card", "mcc 5411 cardRefund", "merchantCountry ZA card", "merchantCountry ZA cardRefund"}
	if !slices.Equal(seen, wantSeen) {
		t.Errorf("seen %v, want %v: values first used after the tx are left out", seen, wantSeen)
	}

	var amounts []int64
	for _, a := range p.OfKinds([]risk.TxKind{risk.KindCard}).Amounts {
		amounts = append(amounts, a.Size)
	}
	if !slices.Equal(amounts, []int64{400, 300}) {
		t.Errorf("last card amounts %v, want [400 300]: the 2 most recent within the lookback", amounts)
	}

	var recent []int64
	for _, r := range p.Recent {
		recent = append(recent, r.Amount)
	}
	if !slices.Equal(recent, []int64{-400, 500}) {
		t.Errorf("recent %v, want [-400 500]: posted in the last hour up to the tx", recent)
	}

	refunds := p.OfKinds([]risk.TxKind{risk.KindCardRefund})
	if len(refunds.Hours) != 1 || len(refunds.Seen) != 2 || len(refunds.Amounts) != 1 || len(refunds.Recent) != 1 {
		t.Errorf("OfKinds(cardRefund) = %+v, want only the refund's entries", refunds)
	}
}
