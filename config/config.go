package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Bands    Bands          `json:"bands"`
	Rules    []RuleInstance `json:"rules"`
	History  History        `json:"history"`
	Consumer Consumer       `json:"consumer"`
}

type Consumer struct {
	Topic           string `json:"topic"`
	Group           string `json:"group"`
	DeadLetterTopic string `json:"deadLetterTopic"`
	Retry           Retry  `json:"retry"`

	Workers                int `json:"workers"`
	MaxPendingPerPartition int `json:"maxPendingPerPartition"`
}

type Retry struct {
	FailAfterAttempts int      `json:"failAfterAttempts"`
	InitialBackoff    Duration `json:"initialBackoff"`
	MaxBackoff        Duration `json:"maxBackoff"`
}

type Bands struct {
	MinMedium int `json:"minMedium"`
	MinHigh   int `json:"minHigh"`
}

type History struct {
	Lookback Duration `json:"lookback"`
}

type Duration time.Duration

const durationHint = `use a Go duration string like "15m", "24h" or "2160h" (there is no days unit)`

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("invalid duration %s: %s", b, durationHint)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %s", s, durationHint)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error
	require := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	require(c.Bands.MinMedium > 0, "bands.minMedium must be > 0")
	require(c.Bands.MinHigh > c.Bands.MinMedium, "bands.minHigh must be > bands.minMedium")
	require(c.Bands.MinHigh <= 100, "bands.minHigh must be <= 100")

	require(c.History.Lookback > 0, "history.lookback must be > 0")

	require(c.Consumer.Topic != "", "consumer.topic must be set")
	require(c.Consumer.Group != "", "consumer.group must be set")
	require(c.Consumer.DeadLetterTopic != "", "consumer.deadLetterTopic must be set")
	require(c.Consumer.DeadLetterTopic != c.Consumer.Topic, "consumer.deadLetterTopic must differ from consumer.topic")
	require(c.Consumer.Workers >= 1, "consumer.workers must be >= 1")
	require(c.Consumer.MaxPendingPerPartition >= 1, "consumer.maxPendingPerPartition must be >= 1")
	r := c.Consumer.Retry
	require(r.FailAfterAttempts >= 1, "consumer.retry.failAfterAttempts must be >= 1")
	require(r.InitialBackoff > 0, "consumer.retry.initialBackoff must be > 0")
	require(r.MaxBackoff >= r.InitialBackoff, "consumer.retry.maxBackoff must be >= consumer.retry.initialBackoff")

	errs = append(errs, validateRules(c.Rules, c.History.Lookback)...)

	return errors.Join(errs...)
}
