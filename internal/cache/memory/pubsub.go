package memory

import (
	"context"
	"sync"
)

type PubSub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan []byte]struct{}
}

func NewPubSub() *PubSub {
	return &PubSub{subscribers: make(map[string]map[chan []byte]struct{})}
}

func (p *PubSub) Publish(ctx context.Context, channel string, payload []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	p.mu.RLock()
	subs := p.subscribers[channel]
	channels := make([]chan []byte, 0, len(subs))
	for ch := range subs {
		channels = append(channels, ch)
	}
	p.mu.RUnlock()

	for _, ch := range channels {
		msg := append([]byte(nil), payload...)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- msg:
		default:
			go func(out chan []byte) {
				select {
				case <-ctx.Done():
				case out <- msg:
				}
			}(ch)
		}
	}
	return nil
}

func (p *PubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
	}

	ch := make(chan []byte, 16)

	p.mu.Lock()
	if p.subscribers[channel] == nil {
		p.subscribers[channel] = make(map[chan []byte]struct{})
	}
	p.subscribers[channel][ch] = struct{}{}
	p.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			p.mu.Lock()
			delete(p.subscribers[channel], ch)
			if len(p.subscribers[channel]) == 0 {
				delete(p.subscribers, channel)
			}
			p.mu.Unlock()
			close(ch)
		})
	}

	go func() {
		<-ctx.Done()
		unsubscribe()
	}()

	return ch, unsubscribe, nil
}
