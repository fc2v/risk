package consumer_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"risk/risk"
	"risk/risk/consumer"
	"risk/risk/contract"
	"risk/risk/engine"
	"risk/risk/ops/opstest"
)

func newConsumer() (*consumer.Consumer, *opstest.Backends) {
	b := opstest.New(engine.New(engine.Bands{MinMedium: 30, MinHigh: 60}))
	return consumer.NewConsumer(b), b
}

func sample(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../contract/testdata/incoming_tx_" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func payload(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func with(m map[string]any, key string, value any) map[string]any {
	if value == nil {
		delete(m, key)
	} else {
		m[key] = value
	}
	return m
}

func TestHandleValidSamples(t *testing.T) {
	for _, name := range []string{"card", "debit_order", "transfer"} {
		t.Run(name, func(t *testing.T) {
			c, b := newConsumer()
			if err := c.Handle(t.Context(), payload(t, sample(t, name))); err != nil {
				t.Fatal(err)
			}
			if len(b.Pub.Messages) != 1 || len(b.Store.Txs) != 1 {
				t.Errorf("published %d, stored %d; want 1 each", len(b.Pub.Messages), len(b.Store.Txs))
			}
		})
	}
}

func TestHandleRejectsInvalid(t *testing.T) {
	cases := map[string]func(t *testing.T) []byte{
		"not json":                func(*testing.T) []byte { return []byte("{") },
		"wrong type for amount":   func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "amount", "ten")) },
		"unknown transactionType": func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "transactionType", "cheque")) },
		"no id":                   func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "id", nil)) },
		"zero amount":             func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "amount", 0)) },
		"no postedAt":             func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "postedAt", nil)) },
		"mcc not 4 digits":        func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "mcc", "54x1")) },
		"country not alpha-2":     func(t *testing.T) []byte { return payload(t, with(sample(t, "card"), "merchantCountry", "ZAF")) },
		"no mandateId":            func(t *testing.T) []byte { return payload(t, with(sample(t, "debit_order"), "mandateId", nil)) },
		"actionDate not a date": func(t *testing.T) []byte {
			return payload(t, with(sample(t, "debit_order"), "actionDate", "09/11/2026"))
		},
		"no counterpartyType":      func(t *testing.T) []byte { return payload(t, with(sample(t, "transfer"), "counterpartyType", nil)) },
		"unknown counterpartyType": func(t *testing.T) []byte { return payload(t, with(sample(t, "transfer"), "counterpartyType", "own")) },
		"no counterpartyAccountHash": func(t *testing.T) []byte {
			return payload(t, with(sample(t, "transfer"), "counterpartyAccountHash", nil))
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			c, b := newConsumer()
			err := c.Handle(t.Context(), build(t))
			if !errors.Is(err, consumer.ErrUnprocessable) || !errors.Is(err, consumer.ErrInvalid) {
				t.Errorf("error = %v, want ErrUnprocessable and ErrInvalid", err)
			}
			if len(b.Pub.Messages) != 0 {
				t.Error("invalid tx was published")
			}
		})
	}
}

func TestHandleConflictIsUnprocessable(t *testing.T) {
	c, _ := newConsumer()
	original := sample(t, "card")
	if err := c.Handle(t.Context(), payload(t, original)); err != nil {
		t.Fatal(err)
	}
	err := c.Handle(t.Context(), payload(t, with(original, "amount", -1)))
	if !errors.Is(err, consumer.ErrUnprocessable) || !errors.Is(err, risk.ErrConflictingTx) || errors.Is(err, consumer.ErrInvalid) {
		t.Errorf("error = %v, want ErrUnprocessable and ErrConflictingTx only", err)
	}
}

func TestHandleInfrastructureErrorIsRetryable(t *testing.T) {
	c, b := newConsumer()
	b.Store.GetErr = errors.New("connection reset")
	err := c.Handle(t.Context(), payload(t, sample(t, "card")))
	if err == nil || errors.Is(err, consumer.ErrUnprocessable) {
		t.Errorf("error = %v, want a retryable error", err)
	}
}

func TestFailThrough(t *testing.T) {
	cases := map[string]struct {
		cause      error
		wantReason string
	}{
		"invalid":    {fmt.Errorf("%w: %w: bad mcc", consumer.ErrUnprocessable, consumer.ErrInvalid), "INVALID"},
		"conflict":   {fmt.Errorf("%w: %w", consumer.ErrUnprocessable, risk.ErrConflictingTx), "CONFLICT"},
		"unexpected": {errors.New("pq: something odd"), "INTERNAL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, b := newConsumer()
			if err := c.FailThrough(t.Context(), payload(t, sample(t, "card")), tc.cause); err != nil {
				t.Fatal(err)
			}
			var got contract.EvaluatedTx
			if err := json.Unmarshal(b.Pub.Messages[0].Payload, &got); err != nil {
				t.Fatal(err)
			}
			if got.Risk != "UNKNOWN" || got.FailureReason != tc.wantReason {
				t.Errorf("published risk %s failureReason %s, want UNKNOWN %s", got.Risk, got.FailureReason, tc.wantReason)
			}
			if got.PostedAt.Location().String() != "UTC" {
				t.Errorf("postedAt %v not UTC", got.PostedAt)
			}
		})
	}

	for name, body := range map[string][]byte{
		"not json":   []byte("{"),
		"no id":      payload(t, with(sample(t, "card"), "id", nil)),
		"no account": payload(t, with(sample(t, "card"), "accountId", nil)),
	} {
		t.Run(name, func(t *testing.T) {
			c, b := newConsumer()
			if err := c.FailThrough(t.Context(), body, errors.New("cause")); !errors.Is(err, consumer.ErrUnpublishable) {
				t.Errorf("error = %v, want ErrUnpublishable", err)
			}
			if len(b.Pub.Messages) != 0 {
				t.Error("published without an id or account")
			}
		})
	}
}
