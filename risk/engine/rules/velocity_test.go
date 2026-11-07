package rules

import (
	"testing"
	"time"

	"risk/risk"
)

func TestVelocity(t *testing.T) {
	r := Velocity{Window: time.Hour, Multiplier: 4, MinCount: 5, MinBaselineTxs: 10, MinBaselineSpan: 7 * 24 * time.Hour, Score: 25}
	spend := func(at time.Time) risk.Tx { return card(-1000, at) }
	dailyFor2Weeks := before(14, 24*time.Hour, now, spend)
	windowStart := now.Add(-time.Hour)

	runCases(t, r, []ruleCase{
		{
			name:  "burst well above a daily habit",
			tx:    spend(now),
			prior: append(before(4, 10*time.Minute, now, spend), dailyFor2Weeks...),
			want:  risk.ReasonVelocityAboveNormal,
		},
		{
			name:  "fewer than minCount in the window",
			tx:    spend(now),
			prior: append(before(3, 10*time.Minute, now, spend), dailyFor2Weeks...),
			want:  risk.ReasonVelocityWithinNormal,
		},
		{
			name:  "too few baseline txs",
			tx:    spend(now),
			prior: append(before(4, 10*time.Minute, now, spend), before(9, 24*time.Hour, now, spend)...),
			want:  risk.ReasonInsufficientHistory,
		},
		{
			name:  "baseline span too short",
			tx:    spend(now),
			prior: append(before(4, 10*time.Minute, now, spend), before(14, 5*time.Hour, windowStart, spend)...),
			want:  risk.ReasonInsufficientHistory,
		},
		{
			name:  "busy account at its normal rate",
			tx:    spend(now),
			prior: append(before(5, 10*time.Minute, now, spend), before(8*24*5, 12*time.Minute, windowStart, spend)...),
			want:  risk.ReasonVelocityWithinNormal,
		},
		{
			name:  "txs posted after this one are ignored",
			tx:    spend(now),
			prior: append(before(4, -10*time.Minute, now, spend), dailyFor2Weeks...),
			want:  risk.ReasonVelocityWithinNormal,
		},
	})
}
