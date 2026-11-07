package kafka

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"risk/config"
	"risk/risk/consumer"
)

type fakeHandler struct {
	mu        sync.Mutex
	errs      []error // per attempt; the last repeats
	panics    bool
	calls     int
	failedFor []error
	failErrs  []error // per FailThrough call; the last repeats
}

func (h *fakeHandler) Handle(context.Context, []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.panics {
		panic("nil map")
	}
	return pick(h.errs, h.calls)
}

func (h *fakeHandler) FailThrough(_ context.Context, _ []byte, cause error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failedFor = append(h.failedFor, cause)
	return pick(h.failErrs, len(h.failedFor))
}

func pick(errs []error, n int) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[min(n, len(errs))-1]
}

type fakeClient struct {
	mu          sync.Mutex
	produced    []*kgo.Record
	produceErrs []error
	pingErr     error
}

func (c *fakeClient) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.produced = append(c.produced, rs...)
	err := pick(c.produceErrs, len(c.produced))
	return kgo.ProduceResults{{Record: rs[0], Err: err}}
}

func (c *fakeClient) Ping(context.Context) error { return c.pingErr }

type fakeMetrics struct {
	mu                      sync.Mutex
	failures                map[string]int
	failedThrough, deadOnly int
}

func (m *fakeMetrics) SettleFailed(kind string) { m.mu.Lock(); m.failures[kind]++; m.mu.Unlock() }
func (m *fakeMetrics) TxFailedThrough()         { m.mu.Lock(); m.failedThrough++; m.mu.Unlock() }
func (m *fakeMetrics) TxDeadLettered()          { m.mu.Lock(); m.deadOnly++; m.mu.Unlock() }

type harness struct {
	s       *settler
	handler *fakeHandler
	client  *fakeClient
	metrics *fakeMetrics
	dbErr   error
}

func newHarness() *harness {
	h := &harness{handler: &fakeHandler{}, client: &fakeClient{}, metrics: &fakeMetrics{failures: map[string]int{}}}
	h.s = &settler{
		handler:         h.handler,
		client:          h.client,
		pingDB:          func(context.Context) error { return h.dbErr },
		deadLetterTopic: "txs.incoming.dlq",
		retry: config.Retry{
			FailAfterAttempts: 3,
			InitialBackoff:    config.Duration(time.Millisecond),
			MaxBackoff:        config.Duration(2 * time.Millisecond),
		},
		metrics: h.metrics,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h
}

var record = &kgo.Record{
	Topic: "txs.incoming", Partition: 2, Offset: 41,
	Key: []byte("acc-1"), Value: []byte(`{"id":"tx-1"}`),
	Headers: []kgo.RecordHeader{{Key: "trace", Value: []byte("abc")}},
}

var (
	unprocessable = fmt.Errorf("%w: %w: bad mcc", consumer.ErrUnprocessable, consumer.ErrInvalid)
	transient     = driver.ErrBadConn
	unexpected    = errors.New("something odd")
)

func TestSettleSuccess(t *testing.T) {
	h := newHarness()
	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if h.handler.calls != 1 || len(h.client.produced) != 0 || len(h.handler.failedFor) != 0 {
		t.Errorf("calls %d, dead-lettered %d, failed through %d; want 1, 0, 0",
			h.handler.calls, len(h.client.produced), len(h.handler.failedFor))
	}
}

func TestTransientErrorsNeverFailThrough(t *testing.T) {
	h := newHarness()
	h.handler.errs = []error{transient, transient, transient, transient, transient, transient, nil}

	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if h.handler.calls != 7 || len(h.handler.failedFor) != 0 || h.metrics.failures[FailureTransient] != 6 {
		t.Errorf("calls %d, failed through %d, transient failures %d; want 7, 0, 6",
			h.handler.calls, len(h.handler.failedFor), h.metrics.failures[FailureTransient])
	}
}

func TestUnexpectedErrorsFailThroughAfterLimitWhenHealthy(t *testing.T) {
	h := newHarness()
	h.handler.errs = []error{unexpected}

	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if h.handler.calls != 3 || len(h.handler.failedFor) != 1 || h.metrics.failedThrough != 1 {
		t.Errorf("calls %d, failed through %d, metric %d; want 3, 1, 1",
			h.handler.calls, len(h.handler.failedFor), h.metrics.failedThrough)
	}
	if !errors.Is(h.handler.failedFor[0], unexpected) {
		t.Errorf("failed through with cause %v, want the handler's error", h.handler.failedFor[0])
	}
}

func TestOutageNeverFailsThrough(t *testing.T) {
	for name, outage := range map[string]func(h *harness){
		"db down":    func(h *harness) { h.dbErr = errors.New("db unreachable") },
		"kafka down": func(h *harness) { h.client.pingErr = errors.New("no brokers") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.handler.errs = []error{unexpected}
			outage(h)

			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			if h.s.settle(ctx, record) {
				t.Fatal("settled during an outage")
			}
			if h.handler.calls <= 3 || len(h.client.produced) != 0 || len(h.handler.failedFor) != 0 {
				t.Errorf("calls %d, dead-lettered %d, failed through %d; want >3 retries and nothing failed through",
					h.handler.calls, len(h.client.produced), len(h.handler.failedFor))
			}
		})
	}
}

func TestUnprocessableFailsThroughImmediately(t *testing.T) {
	h := newHarness()
	h.handler.errs = []error{unprocessable}

	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if h.handler.calls != 1 || len(h.handler.failedFor) != 1 {
		t.Fatalf("calls %d, failed through %d; want 1, 1", h.handler.calls, len(h.handler.failedFor))
	}

	dlq := h.client.produced[0]
	if dlq.Topic != "txs.incoming.dlq" || string(dlq.Key) != "acc-1" || string(dlq.Value) != `{"id":"tx-1"}` {
		t.Errorf("dead-letter copy topic %s key %s value %s", dlq.Topic, dlq.Key, dlq.Value)
	}
	if got := headers(dlq); got["trace"] != "abc" || got["failure-reason"] != "INVALID" || !strings.Contains(got["error"], "bad mcc") {
		t.Errorf("dead-letter headers %v, want the original headers plus failure-reason INVALID and the error", got)
	}
	if len(record.Headers) != 1 {
		t.Error("dead-lettering changed the original record's headers")
	}
}

func TestUnpublishableIsDeadLetteredOnly(t *testing.T) {
	h := newHarness()
	h.handler.errs = []error{unprocessable}
	h.handler.failErrs = []error{consumer.ErrUnpublishable}

	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if len(h.client.produced) != 1 || h.metrics.deadOnly != 1 || h.metrics.failedThrough != 0 {
		t.Errorf("dead-lettered %d, dead-only metric %d, failed-through metric %d; want 1, 1, 0",
			len(h.client.produced), h.metrics.deadOnly, h.metrics.failedThrough)
	}
}

func TestFailThroughRetriesUntilItLands(t *testing.T) {
	t.Run("dead-letter write fails first", func(t *testing.T) {
		h := newHarness()
		h.handler.errs = []error{unprocessable}
		h.client.produceErrs = []error{kgo.ErrRecordTimeout, nil}

		if !h.s.settle(t.Context(), record) {
			t.Fatal("not settled")
		}
		if len(h.handler.failedFor) != 1 {
			t.Errorf("published UNKNOWN %d times, want once, after the dead-letter copy landed", len(h.handler.failedFor))
		}
	})

	t.Run("UNKNOWN publish fails first", func(t *testing.T) {
		h := newHarness()
		h.handler.errs = []error{unprocessable}
		h.handler.failErrs = []error{kgo.ErrRecordTimeout, nil}

		if !h.s.settle(t.Context(), record) {
			t.Fatal("not settled")
		}
		if len(h.handler.failedFor) != 2 {
			t.Errorf("FailThrough called %d times, want 2", len(h.handler.failedFor))
		}
	})
}

func TestPanicFailsThrough(t *testing.T) {
	h := newHarness()
	h.handler.panics = true

	if !h.s.settle(t.Context(), record) {
		t.Fatal("not settled")
	}
	if len(h.handler.failedFor) != 1 || !errors.Is(h.handler.failedFor[0], consumer.ErrUnprocessable) ||
		!strings.Contains(h.handler.failedFor[0].Error(), "nil map") {
		t.Errorf("failed through with %v, want an unprocessable panic", h.handler.failedFor)
	}
	if got := headers(h.client.produced[0])["failure-reason"]; got != "INTERNAL" {
		t.Errorf("dead-letter failure-reason %q, want INTERNAL", got)
	}
}

func TestBackoffDoublesToMax(t *testing.T) {
	s := &settler{retry: config.Retry{InitialBackoff: config.Duration(time.Second), MaxBackoff: config.Duration(time.Minute)}}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := s.backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %s, want %s", i+1, got, w)
		}
	}
	if got := s.backoff(1000); got != time.Minute {
		t.Errorf("backoff(1000) = %s, want the max", got)
	}
}

func TestShutdownStopsRetrying(t *testing.T) {
	h := newHarness()
	h.handler.errs = []error{transient}
	h.s.retry.InitialBackoff = config.Duration(time.Hour)
	h.s.retry.MaxBackoff = config.Duration(time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)
	start := time.Now()
	if h.s.settle(ctx, record) {
		t.Fatal("settled after shutdown")
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("shutdown took %s; the backoff sleep should end at once", took)
	}
}

func headers(r *kgo.Record) map[string]string {
	m := map[string]string{}
	for _, h := range r.Headers {
		m[h.Key] = string(h.Value)
	}
	return m
}
