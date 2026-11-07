package kafka

import (
	"context"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"

	"risk/config"
)

// Consumer hands records to a dispatcher that settles accounts concurrently, so a slow account holds
// up only its own records and those of accounts sharing its worker.
type Consumer struct {
	client   *kgo.Client
	dispatch *dispatcher
	logger   *slog.Logger
}

func NewConsumer(conn Connection, cfg config.Consumer, handler Handler, pingDB func(context.Context) error, metrics Metrics, logger *slog.Logger) (*Consumer, error) {
	opts, err := conn.options()
	if err != nil {
		return nil, err
	}

	c := &Consumer{logger: logger}
	client, err := kgo.NewClient(append(opts,
		kgo.ConsumerGroup(cfg.Group),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.AutoCommitMarks(),
		kgo.BlockRebalanceOnPoll(),
		kgo.OnPartitionsAssigned(c.assigned),
		kgo.OnPartitionsRevoked(c.revoked),
		kgo.OnPartitionsLost(c.lost),
	)...)
	if err != nil {
		return nil, err
	}

	s := &settler{
		handler:         handler,
		client:          client,
		pingDB:          pingDB,
		deadLetterTopic: cfg.DeadLetterTopic,
		retry:           cfg.Retry,
		metrics:         metrics,
		logger:          logger,
	}
	c.client = client
	c.dispatch = newDispatcher(client, s.settle, cfg.Workers, cfg.MaxPendingPerPartition)
	return c, nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return ctx.Err()
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			c.logger.Error("fetch failed", "topic", topic, "partition", partition, "err", err)
		})
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			c.dispatch.dispatch(p.Topic, p.Partition, p.Records)
		})
		c.client.AllowRebalance()
	}
}

// Close leaves the group, which revokes and commits every partition, then stops the workers.
// Leaving is a rebalance, so it has to be allowed first or it waits for a poll that never comes.
func (c *Consumer) Close() {
	c.client.AllowRebalance()
	c.client.Close()
	c.dispatch.close()
}

func (c *Consumer) assigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	c.logPartitions(slog.LevelInfo, "partitions assigned", assigned)
	c.dispatch.assign(assigned)
}

func (c *Consumer) revoked(ctx context.Context, client *kgo.Client, revoked map[string][]int32) {
	c.logPartitions(slog.LevelInfo, "partitions revoked", revoked)
	c.dispatch.revoke(revoked)
	if err := client.CommitMarkedOffsets(ctx); err != nil {
		c.logger.Error("commit on revoke failed", "err", err)
	}
}

// Lost partitions already belong to another member, so nothing is committed for them.
func (c *Consumer) lost(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	c.logPartitions(slog.LevelWarn, "partitions lost", lost)
	c.dispatch.revoke(lost)
}

func (c *Consumer) logPartitions(level slog.Level, msg string, partitions map[string][]int32) {
	if len(partitions) > 0 {
		c.logger.Log(context.Background(), level, msg, "partitions", partitions)
	}
}
