package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

type PubSub struct {
	client goredis.UniversalClient
}

func NewPubSub(client goredis.UniversalClient) *PubSub {
	return &PubSub{client: client}
}

func (p *PubSub) Publish(ctx context.Context, channel string, payload []byte) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

func (p *PubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	pubsub := p.client.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan []byte, 16)
	msgs := pubsub.Channel()
	childCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		defer pubsub.Close()
		for {
			select {
			case <-childCtx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				payload := []byte(msg.Payload)
				select {
				case out <- payload:
				case <-childCtx.Done():
					return
				}
			}
		}
	}()
	return out, cancel, nil
}
