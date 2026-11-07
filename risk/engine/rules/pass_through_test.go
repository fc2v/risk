package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestPassThrough(t *testing.T) {
	r := PassThrough{Window: 6 * time.Hour, MinInbound: 500_000, MinShare: 0.8, NewCounterpartyOnly: true, Score: 30}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	in := func(amount int64, at time.Time) risk.Tx { return transfer(amount, at, "sender") }
	out := func(amount int64, payee string, at time.Time) risk.Tx { return transfer(-amount, at, payee) }
	paidKnownLastMonth := out(1000, "known", ago(30*24*time.Hour))

	runCases(t, r, []ruleCase{
		{name: "most of inflow sent on", tx: out(450_000, "new", now), prior: []risk.Tx{in(500_000, ago(2*time.Hour))}, want: risk.ReasonPassThrough},
		{name: "inflow below minimum", tx: out(380_000, "new", now), prior: []risk.Tx{in(400_000, ago(2*time.Hour))}, want: risk.ReasonInflowBelowMinimum},
		{name: "share below minimum", tx: out(300_000, "new", now), prior: []risk.Tx{in(500_000, ago(2*time.Hour))}, want: risk.ReasonShareBelowMinimum},
		{name: "inflow outside window", tx: out(450_000, "new", now), prior: []risk.Tx{in(500_000, ago(7*time.Hour))}, want: risk.ReasonInflowBelowMinimum},
		{
			name:  "outflow split across payees",
			tx:    out(250_000, "new-b", now),
			prior: []risk.Tx{in(500_000, ago(2*time.Hour)), out(200_000, "new-a", ago(time.Hour))},
			want:  risk.ReasonPassThrough,
		},
		{
			name:  "outflow before inflow doesn't count",
			tx:    out(100_000, "new-b", now),
			prior: []risk.Tx{out(400_000, "new-a", ago(3*time.Hour)), in(500_000, ago(2*time.Hour))},
			want:  risk.ReasonShareBelowMinimum,
		},
		{
			name:  "known payee",
			tx:    out(450_000, "known", now),
			prior: []risk.Tx{paidKnownLastMonth, in(500_000, ago(2*time.Hour))},
			want:  risk.ReasonPayeePaidBefore,
		},
	})

	t.Run("any payee", func(t *testing.T) {
		anyPayee := r
		anyPayee.NewCounterpartyOnly = false
		runCases(t, anyPayee, []ruleCase{{
			name:  "known payee",
			tx:    out(450_000, "known", now),
			prior: []risk.Tx{paidKnownLastMonth, in(500_000, ago(2*time.Hour))},
			want:  risk.ReasonPassThrough,
		}})
	})
}
