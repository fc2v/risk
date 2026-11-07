package ops

import (
	"context"
	"log/slog"
	"time"

	"risk/risk"
	"risk/risk/engine"
)

type Backends interface {
	Logger() *slog.Logger
	Engine() *engine.Engine
	EvaluatedTxDB() EvaluatedTxDB
	Publisher() Publisher
	Profiles() Profiles
	ProfileWindow() engine.ProfileWindow
	Metrics() Metrics
}

const (
	StageLoadHistory = "load_history"
	StageEvaluate    = "evaluate"
	StagePublish     = "publish"
	StageInsert      = "insert"
)

type Metrics interface {
	TxEvaluated(et risk.EvaluatedTx)
	TxAlreadyEvaluated()
	StageTook(stage string, d time.Duration)
}

type EvaluatedTxDB interface {
	Get(ctx context.Context, id string) (risk.Tx, bool, error)
	Insert(ctx context.Context, et risk.EvaluatedTx) error
}

type Profiles interface {
	Load(ctx context.Context, accountID string, w engine.ProfileWindow) (engine.Profile, error)
}

type Publisher interface {
	Publish(ctx context.Context, topic string, msg Message) error
}

type Message struct {
	Key     string // ordering
	ID      string // dedupe
	Payload []byte
}
