package kafka

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"

	"risk/risk/ops"
)

const txIDHeader = "tx-id"

type Publisher struct {
	client *kgo.Client
}

func NewPublisher(conn Connection) (*Publisher, error) {
	opts, err := conn.options()
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &Publisher{client: client}, nil
}

func (p *Publisher) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

func (p *Publisher) Close() {
	p.client.Close()
}

func (p *Publisher) Publish(ctx context.Context, topic string, msg ops.Message) error {
	return p.client.ProduceSync(ctx, &kgo.Record{
		Topic:   topic,
		Key:     []byte(msg.Key),
		Value:   msg.Payload,
		Headers: []kgo.RecordHeader{{Key: txIDHeader, Value: []byte(msg.ID)}},
	}).FirstErr()
}
