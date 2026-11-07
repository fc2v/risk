//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func createTopics(ctx context.Context, brokers []string, partitions map[string]int32) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return err
	}
	defer client.Close()

	req := kmsg.NewPtrCreateTopicsRequest()
	req.TimeoutMillis = 30_000
	for name, n := range partitions {
		t := kmsg.NewCreateTopicsRequestTopic()
		t.Topic = name
		t.NumPartitions = n
		t.ReplicationFactor = 1
		req.Topics = append(req.Topics, t)
	}

	resp, err := req.RequestWith(ctx, client)
	if err != nil {
		return fmt.Errorf("create topics: %w", err)
	}
	for _, t := range resp.Topics {
		if err := kerr.ErrorForCode(t.ErrorCode); err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", t.Topic, err)
		}
	}
	return nil
}

type producer struct {
	client *kgo.Client
}

func newProducer(brokers []string) (*producer, error) {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.DefaultProduceTopic(suite.incoming))
	if err != nil {
		return nil, err
	}
	return &producer{client: client}, nil
}

func (p *producer) close() { p.client.Close() }

func produce(t *testing.T, account string, payloads ...[]byte) {
	t.Helper()
	recs := make([]*kgo.Record, len(payloads))
	for i, payload := range payloads {
		recs[i] = &kgo.Record{Key: []byte(account), Value: payload}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := suite.producer.client.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		t.Fatalf("produce: %v", err)
	}
}

// publishedTx is the part of the published contract the tests check.
type publishedTx struct {
	ID            string `json:"id"`
	AccountID     string `json:"accountId"`
	Amount        int64  `json:"amount"`
	Score         int    `json:"score"`
	Risk          string `json:"risk"`
	FailureReason string `json:"failureReason"`
	RuleOutcomes  []struct {
		Rule      string `json:"rule"`
		Triggered bool   `json:"triggered"`
		Reason    string `json:"reason"`
	} `json:"ruleOutcomes"`

	txIDHeader string
}

func (p publishedTx) triggered(rule string) bool {
	for _, o := range p.RuleOutcomes {
		if o.Rule == rule {
			return o.Triggered
		}
	}
	return false
}

type collector struct {
	client *kgo.Client
	cancel context.CancelFunc
	done   chan struct{}

	mu           sync.Mutex
	evaluated    map[string][]publishedTx
	deadLettered map[string][]*kgo.Record
}

func startCollector(brokers []string, since time.Time) (*collector, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(evaluatedTopic, suite.dlq),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(since.UnixMilli())),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &collector{
		client:       client,
		cancel:       cancel,
		done:         make(chan struct{}),
		evaluated:    make(map[string][]publishedTx),
		deadLettered: make(map[string][]*kgo.Record),
	}
	go c.run(ctx)
	return c, nil
}

func (c *collector) run(ctx context.Context) {
	defer close(c.done)
	for {
		fetches := c.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		fetches.EachRecord(c.add)
	}
}

func (c *collector) add(rec *kgo.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := string(rec.Key)

	if rec.Topic != evaluatedTopic {
		c.deadLettered[key] = append(c.deadLettered[key], rec)
		return
	}
	var p publishedTx
	if err := json.Unmarshal(rec.Value, &p); err != nil {
		p.ID = "undecodable: " + err.Error()
	}
	p.txIDHeader = header(rec, "tx-id")
	c.evaluated[key] = append(c.evaluated[key], p)
}

func (c *collector) close() {
	c.cancel()
	<-c.done
	c.client.Close()
}

func evaluatedFor(account string) []publishedTx {
	c := suite.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]publishedTx(nil), c.evaluated[account]...)
}

func evaluatedTx(account, txID string) []publishedTx {
	var out []publishedTx
	for _, p := range evaluatedFor(account) {
		if p.ID == txID {
			out = append(out, p)
		}
	}
	return out
}

func deadLetteredFor(account string) []*kgo.Record {
	c := suite.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*kgo.Record(nil), c.deadLettered[account]...)
}

func header(rec *kgo.Record, key string) string {
	for _, h := range rec.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
