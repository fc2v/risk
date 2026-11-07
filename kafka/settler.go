package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"risk/config"
	"risk/db"
	"risk/risk/consumer"
)

type Handler interface {
	Handle(ctx context.Context, payload []byte) error
	FailThrough(ctx context.Context, payload []byte, cause error) error
}

const failureReasonHeader = "failure-reason"

const (
	FailureUnprocessable = "unprocessable"
	FailureTransient     = "transient"
	FailureUnexpected    = "unexpected"
)

type Metrics interface {
	SettleFailed(kind string)
	TxFailedThrough()
	TxDeadLettered()
}

// settler retries a record until it is evaluated or, if it can't be, published as UNKNOWN with
// the cause and copied to the dead-letter topic for replay. It never skips a record silently.
type deadLetterClient interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
	Ping(ctx context.Context) error
}

type settler struct {
	handler         Handler
	client          deadLetterClient
	pingDB          func(context.Context) error
	deadLetterTopic string
	retry           config.Retry
	metrics         Metrics
	logger          *slog.Logger
}

// settle returns false if ctx ends first; the record is then left uncommitted.
func (s *settler) settle(ctx context.Context, rec *kgo.Record) bool {
	unexpectedFailures := 0
	for failures := 1; ; failures++ {
		err := s.handle(ctx, rec)
		if err == nil {
			return true
		}

		if s.shouldGiveUp(ctx, err, &unexpectedFailures) {
			if err = s.failThrough(ctx, rec, err); err == nil {
				return true
			}
		}

		s.logger.Warn("tx not settled, retrying",
			"partition", rec.Partition, "offset", rec.Offset, "failures", failures, "err", err)
		if !sleep(ctx, s.backoff(failures)) {
			return false
		}
	}
}

func (s *settler) handle(ctx context.Context, rec *kgo.Record) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: panic: %v", consumer.ErrUnprocessable, r)
		}
	}()
	return s.handler.Handle(ctx, rec.Value)
}

// Transient failures never count towards giving up, and nothing fails through while a dependency
// is down; otherwise an outage would turn every tx it touched into UNKNOWN.
func (s *settler) shouldGiveUp(ctx context.Context, err error, unexpectedFailures *int) bool {
	switch {
	case errors.Is(err, consumer.ErrUnprocessable):
		s.metrics.SettleFailed(FailureUnprocessable)
		return true
	case isTransient(err):
		s.metrics.SettleFailed(FailureTransient)
		return false
	default:
		s.metrics.SettleFailed(FailureUnexpected)
		*unexpectedFailures++
		return *unexpectedFailures >= s.retry.FailAfterAttempts && s.dependenciesHealthy(ctx)
	}
}

func (s *settler) failThrough(ctx context.Context, rec *kgo.Record, cause error) error {
	if err := s.deadLetter(ctx, rec, cause); err != nil {
		return err
	}

	err := s.handler.FailThrough(ctx, rec.Value, cause)
	switch {
	case errors.Is(err, consumer.ErrUnpublishable):
		s.metrics.TxDeadLettered()
		s.logger.Error("tx dead-lettered",
			"accountID", string(rec.Key), "partition", rec.Partition, "offset", rec.Offset, "cause", cause)
		return nil
	case err == nil:
		s.metrics.TxFailedThrough()
		s.logger.Error("tx published as UNKNOWN",
			"accountID", string(rec.Key), "partition", rec.Partition, "offset", rec.Offset, "cause", cause)
	}
	return err
}

func (s *settler) deadLetter(ctx context.Context, rec *kgo.Record, cause error) error {
	headers := append(slices.Clip(rec.Headers),
		kgo.RecordHeader{Key: failureReasonHeader, Value: []byte(consumer.FailureReason(cause))},
		kgo.RecordHeader{Key: "error", Value: []byte(cause.Error())},
	)
	return s.client.ProduceSync(ctx, &kgo.Record{
		Topic:   s.deadLetterTopic,
		Key:     rec.Key,
		Value:   rec.Value,
		Headers: headers,
	}).FirstErr()
}

func (s *settler) dependenciesHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.pingDB(ctx) == nil && s.client.Ping(ctx) == nil
}

func (s *settler) backoff(failures int) time.Duration {
	d, maxBackoff := s.retry.InitialBackoff.Std(), s.retry.MaxBackoff.Std()
	for i := 1; i < failures && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

func isTransient(err error) bool {
	return db.IsTransient(err) ||
		kerr.IsRetriable(err) ||
		errors.Is(err, kgo.ErrRecordTimeout) ||
		errors.Is(err, kgo.ErrRecordRetries)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
