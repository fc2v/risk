package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestUnusualTime(t *testing.T) {
	r := UnusualTime{NeighbourHours: 1, MaxShare: 0.05, MinBaselineTxs: 20, ExcludeRecent: 24 * time.Hour, Score: 20}
	spend := func(at time.Time) risk.Tx { return card(-1000, at) }
	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 2, hour, minute, 0, 0, time.UTC) }
	dailyAt := func(n int, hour int) []risk.Tx {
		return before(n, 24*time.Hour, at(hour, 0).Add(-24*time.Hour), spend)
	}
	sast := time.FixedZone("SAST", 2*60*60)

	runCases(t, r, []ruleCase{
		{name: "far from usual hour", tx: spend(at(3, 0)), prior: dailyAt(30, 12), want: risk.ReasonUnusualTime},
		{name: "within neighbour hours", tx: spend(at(13, 0)), prior: dailyAt(30, 12), want: risk.ReasonUsualTime},
		{name: "too few baseline txs", tx: spend(at(3, 0)), prior: dailyAt(10, 12), want: risk.ReasonInsufficientHistory},
		{name: "neighbours wrap past midnight", tx: spend(at(0, 30)), prior: dailyAt(30, 23), want: risk.ReasonUsualTime},
		{
			name:  "recent txs don't count as habit",
			tx:    spend(at(3, 0)),
			prior: append(dailyAt(30, 12), before(10, 5*time.Minute, at(3, 0), spend)...),
			want:  risk.ReasonUnusualTime,
		},
		{name: "hour is taken in UTC", tx: spend(at(3, 0).In(sast)), prior: dailyAt(30, 12), want: risk.ReasonUnusualTime},
	})
}
