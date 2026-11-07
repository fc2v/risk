package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestNewCountry(t *testing.T) {
	r := NewCountry{PaymentChannels: []string{"in store", "online"}, MinBaselineTxs: 5, Score: 30}
	spend := func(country, channel string, at time.Time) risk.Tx {
		tx := card(-1000, at)
		tx.MerchantCountry, tx.PaymentChannel = country, channel
		return tx
	}
	home := func(n int) []risk.Tx {
		return before(n, 24*time.Hour, now, func(at time.Time) risk.Tx { return spend("ZA", "in store", at) })
	}

	runCases(t, r, []ruleCase{
		{name: "new country in store", tx: spend("US", "in store", now), prior: home(5), want: risk.ReasonNewCountry},
		{name: "home country", tx: spend("ZA", "in store", now), prior: home(5), want: risk.ReasonKnownCountry},
		{name: "unlisted channel", tx: spend("US", "atm", now), prior: home(5), want: risk.ReasonChannelNotChecked},
		{name: "too few baseline txs", tx: spend("US", "in store", now), prior: home(4), want: risk.ReasonInsufficientHistory},
		{name: "not a card tx", tx: transfer(-1000, now, "payee"), prior: home(5), want: risk.ReasonNotApplicable},
	})
}
