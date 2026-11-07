// Package opstest provides in-memory ops.Backends for tests.
package opstest

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"risk/risk"
	"risk/risk/engine"
	"risk/risk/ops"
)

// Backends records every publish and insert in Events, in order.
type Backends struct {
	Eng      *engine.Engine
	Store    *Store
	Pub      *Publisher
	Stats    *Metrics
	Lookback time.Duration

	mu     sync.Mutex
	events []string
}

func New(e *engine.Engine) *Backends {
	b := &Backends{Eng: e, Lookback: 90 * 24 * time.Hour, Stats: &Metrics{}}
	b.Store = &Store{b: b, Txs: map[string]risk.EvaluatedTx{}}
	b.Pub = &Publisher{b: b}
	return b
}

func (b *Backends) Logger() *slog.Logger             { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (b *Backends) Engine() *engine.Engine           { return b.Eng }
func (b *Backends) EvaluatedTxDB() ops.EvaluatedTxDB { return b.Store }
func (b *Backends) Publisher() ops.Publisher         { return b.Pub }
func (b *Backends) Profiles() ops.Profiles           { return storeProfiles{b.Store} }
func (b *Backends) ProfileWindow() engine.ProfileWindow {
	return engine.ProfileWindow{Lookback: b.Lookback, RecentWindow: 24 * time.Hour, LastAmounts: 200}
}

type storeProfiles struct{ s *Store }

func (p storeProfiles) Load(_ context.Context, accountID string, w engine.ProfileWindow) (engine.Profile, error) {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	var txs []risk.Tx
	for _, et := range p.s.Txs {
		if et.AccountID == accountID {
			txs = append(txs, et.Tx)
		}
	}
	return engine.BuildProfile(txs, w), nil
}

func (b *Backends) Metrics() ops.Metrics { return b.Stats }

func (b *Backends) Events() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.events)
}

func (b *Backends) record(event string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

// Store mirrors db.EvaluatedTxModel: Insert of an existing id is ErrDuplicateTx, history is
// (from, to] excluding the given id. GetErr and InsertErr, when set, are returned instead.
type Store struct {
	b         *Backends
	mu        sync.Mutex
	Txs       map[string]risk.EvaluatedTx
	GetErr    error
	InsertErr error
}

func (s *Store) Get(_ context.Context, id string) (risk.Tx, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GetErr != nil {
		return risk.Tx{}, false, s.GetErr
	}
	et, ok := s.Txs[id]
	return et.Tx, ok, nil
}

func (s *Store) Insert(_ context.Context, et risk.EvaluatedTx) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.InsertErr != nil {
		return s.InsertErr
	}
	if _, ok := s.Txs[et.ID]; ok {
		return risk.ErrDuplicateTx
	}
	s.Txs[et.ID] = et
	s.b.record("insert " + et.ID)
	return nil
}

type Publisher struct {
	b        *Backends
	mu       sync.Mutex
	Messages []ops.Message
	Err      error
}

func (p *Publisher) Publish(_ context.Context, topic string, msg ops.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Err != nil {
		return p.Err
	}
	p.Messages = append(p.Messages, msg)
	p.b.record("publish " + msg.ID)
	return nil
}

type Metrics struct {
	mu        sync.Mutex
	Evaluated []risk.EvaluatedTx
	Skipped   int
	Stages    []string
}

func (m *Metrics) TxEvaluated(et risk.EvaluatedTx) {
	m.mu.Lock()
	m.Evaluated = append(m.Evaluated, et)
	m.mu.Unlock()
}
func (m *Metrics) TxAlreadyEvaluated() { m.mu.Lock(); m.Skipped++; m.mu.Unlock() }
func (m *Metrics) StageTook(stage string, _ time.Duration) {
	m.mu.Lock()
	m.Stages = append(m.Stages, stage)
	m.mu.Unlock()
}
