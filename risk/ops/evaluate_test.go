package ops_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"risk/risk"
	"risk/risk/contract"
	"risk/risk/engine"
	"risk/risk/engine/rules"
	"risk/risk/ops"
	"risk/risk/ops/opstest"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func newBackends(t *testing.T) *opstest.Backends {
	t.Helper()
	scoped, err := engine.Scope("highAmountSpend",
		rules.HighAmount{Multiplier: 3, MinBaselineTxs: 3, MedianOfLast: 200, DefaultThreshold: 500_000, Score: 40},
		[]risk.TxKind{risk.KindCard}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return opstest.New(engine.New(engine.Bands{MinMedium: 30, MinHigh: 60}, scoped))
}

func card(id string, amount int64, at time.Time) risk.Tx {
	return risk.Tx{ID: id, AccountID: "acc-1", Type: risk.TxTypeCard, Amount: amount, PostedAt: at,
		Card: &risk.Card{MCC: "5411", PaymentChannel: "in store", MerchantName: "Shop", MerchantCountry: "ZA"}}
}

func store(b *opstest.Backends, txs ...risk.Tx) {
	for _, tx := range txs {
		b.Store.Txs[tx.ID] = risk.EvaluatedTx{Tx: tx, Risk: risk.RiskLow}
	}
}

func published(t *testing.T, b *opstest.Backends, i int) contract.EvaluatedTx {
	t.Helper()
	var got contract.EvaluatedTx
	if err := json.Unmarshal(b.Pub.Messages[i].Payload, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEvaluatePublishesThenInserts(t *testing.T) {
	b := newBackends(t)
	tx := card("tx-1", -600_000, now)
	if err := ops.EvaluateTx(t.Context(), b, tx); err != nil {
		t.Fatal(err)
	}

	if got, want := b.Events(), []string{"publish tx-1", "insert tx-1"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	msg := b.Pub.Messages[0]
	if msg.Key != "acc-1" || msg.ID != "tx-1" {
		t.Errorf("message key %q id %q, want acc-1 and tx-1", msg.Key, msg.ID)
	}
	got := published(t, b, 0)
	if got.Risk != "MEDIUM" || got.Score != 40 || got.FailureReason != "" {
		t.Errorf("published risk %s score %d failureReason %q, want MEDIUM 40 none", got.Risk, got.Score, got.FailureReason)
	}
	if stored := b.Store.Txs["tx-1"]; stored.Risk != risk.RiskMedium {
		t.Errorf("stored risk %s, want MEDIUM", stored.Risk)
	}
	if len(b.Stats.Evaluated) != 1 {
		t.Errorf("TxEvaluated called %d times, want 1", len(b.Stats.Evaluated))
	}
}

func TestEvaluateLoadsHistoryInsideLookback(t *testing.T) {
	b := newBackends(t)
	b.Lookback = 30 * 24 * time.Hour
	store(b,
		card("in-window-1", -10_000, now.Add(-24*time.Hour)),
		card("in-window-2", -10_000, now.Add(-48*time.Hour)),
		card("in-window-3", -10_000, now.Add(-72*time.Hour)),
		card("too-old-1", -1_000_000, now.Add(-31*24*time.Hour)),
		card("too-old-2", -1_000_000, now.Add(-32*24*time.Hour)),
		card("posted-later-1", -1_000_000, now.Add(time.Hour)),
		card("posted-later-2", -1_000_000, now.Add(2*time.Hour)),
	)
	other := card("other-account", -1_000_000, now.Add(-time.Hour))
	other.AccountID = "acc-2"
	store(b, other)

	if err := ops.EvaluateTx(t.Context(), b, card("tx-1", -40_000, now)); err != nil {
		t.Fatal(err)
	}
	if got := published(t, b, 0); got.Risk != "MEDIUM" {
		t.Errorf("risk %s, want MEDIUM: 40,000 is above 3x the 10,000 median of the 3 txs in the window", got.Risk)
	}
}

func TestEvaluateRedelivery(t *testing.T) {
	cases := map[string]struct {
		redelivered risk.Tx
		wantErr     error
		wantSkipped int
	}{
		"identical is skipped":             {card("tx-1", -5000, now), nil, 1},
		"sub-microsecond time is the same": {card("tx-1", -5000, now.Add(300*time.Nanosecond)), nil, 1},
		"different amount conflicts":       {card("tx-1", -9000, now), risk.ErrConflictingTx, 0},
		"different time conflicts":         {card("tx-1", -5000, now.Add(time.Second)), risk.ErrConflictingTx, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBackends(t)
			store(b, card("tx-1", -5000, now))

			err := ops.EvaluateTx(t.Context(), b, tc.redelivered)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
			if len(b.Pub.Messages) != 0 {
				t.Errorf("published %d messages, want none", len(b.Pub.Messages))
			}
			if b.Stats.Skipped != tc.wantSkipped {
				t.Errorf("skipped %d, want %d", b.Stats.Skipped, tc.wantSkipped)
			}
		})
	}
}

func TestEvaluateDoesNotInsertWhenPublishFails(t *testing.T) {
	b := newBackends(t)
	b.Pub.Err = errors.New("broker down")

	if err := ops.EvaluateTx(t.Context(), b, card("tx-1", -5000, now)); err == nil {
		t.Fatal("no error when publish failed")
	}
	if len(b.Store.Txs) != 0 {
		t.Error("tx stored although it was never published")
	}
}

func TestPublishFailure(t *testing.T) {
	b := newBackends(t)
	if err := ops.PublishFailure(t.Context(), b, card("tx-1", -5000, now), risk.FailureInvalid); err != nil {
		t.Fatal(err)
	}

	got := published(t, b, 0)
	if got.Risk != "UNKNOWN" || got.FailureReason != "INVALID" || got.Score != 0 || len(got.RuleOutcomes) != 0 {
		t.Errorf("published %+v, want UNKNOWN with failureReason INVALID and no outcomes", got)
	}
	if b.Pub.Messages[0].Key != "acc-1" || b.Pub.Messages[0].ID != "tx-1" {
		t.Errorf("message key %q id %q", b.Pub.Messages[0].Key, b.Pub.Messages[0].ID)
	}
	if len(b.Store.Txs) != 0 {
		t.Error("failed-through tx was stored")
	}
}
