package events

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"strings"
	"sync"
	"time"
)

const StreamPrefix = "framework:events:preview:v1:"
const Stream = StreamPrefix + "framework_worker"
const Group = "preview.receipt.v1"

// RedisTransport is replaceable. PostgreSQL retransmits until a committed inbox
// receipt, so stream trimming/reset cannot erase authoritative delivery intent.
type RedisTransport struct {
	client       *redis.Client
	worker       string
	stream       string
	readMu       sync.Mutex
	cursor       string
	pendingFirst bool
}

func OpenRedis(raw string, audience ...string) (*RedisTransport, error) {
	principal := "framework_worker"
	if len(audience) > 1 {
		return nil, ErrDelivery
	}
	if len(audience) == 1 {
		principal = audience[0]
	}
	if !audiencePattern.MatchString(principal) {
		return nil, ErrDelivery
	}
	options, err := redis.ParseURL(raw)
	if err != nil {
		return nil, ErrDelivery
	}
	options.MaxRetries = -1
	options.DialTimeout = time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = time.Second
	options.ContextTimeoutEnabled = true
	id, err := NewID()
	if err != nil {
		return nil, ErrDelivery
	}
	return &RedisTransport{client: redis.NewClient(options), worker: id, stream: StreamPrefix + principal}, nil
}
func (t *RedisTransport) Close() error { return t.client.Close() }
func (t *RedisTransport) Ping(ctx context.Context) error {
	if t.client.Ping(ctx).Err() != nil {
		return ErrDelivery
	}
	return nil
}
func (t *RedisTransport) Publish(ctx context.Context, p Pointer) error {
	raw, err := p.Bytes()
	if err != nil {
		return err
	}
	if !audiencePattern.MatchString(p.Audience) {
		return ErrDelivery
	}
	if t.client.XAdd(ctx, &redis.XAddArgs{Stream: StreamPrefix + p.Audience, MaxLen: 10000, Approx: true, Values: map[string]any{"pointer": string(raw)}}).Err() != nil {
		return ErrDelivery
	}
	return nil
}
func (t *RedisTransport) Read(ctx context.Context) (Message, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	err := t.client.XGroupCreateMkStream(ctx, t.stream, Group, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return Message{}, ErrDelivery
	}
	// Alternate fresh and pending reads, and advance the reclaim cursor. A failed
	// older message cannot monopolize every read across unrelated aggregates.
	t.pendingFirst = !t.pendingFirst
	if !t.pendingFirst {
		m, e := t.readNew(ctx, -1)
		if e != nil || m.ID != "" {
			return m, e
		}
	}
	if t.cursor == "" {
		t.cursor = "0-0"
	}
	pending, next, err := t.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: t.stream, Group: Group, Consumer: t.worker, MinIdle: time.Second, Start: t.cursor, Count: 1}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return Message{}, ErrDelivery
	}
	if len(pending) > 0 {
		t.cursor = next
		return message(pending[0]), nil
	}
	t.cursor = next
	return t.readNew(ctx, 500*time.Millisecond)
}
func (t *RedisTransport) readNew(ctx context.Context, block time.Duration) (Message, error) {
	streams, err := t.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: Group, Consumer: t.worker, Streams: []string{t.stream, ">"}, Count: 1, Block: block}).Result()
	if errors.Is(err, redis.Nil) {
		return Message{}, nil
	}
	if err != nil {
		return Message{}, ErrDelivery
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return Message{}, nil
	}
	return message(streams[0].Messages[0]), nil
}
func message(m redis.XMessage) Message {
	raw, ok := m.Values["pointer"].(string)
	if !ok || len(m.Values) != 1 || len(raw) > 1024 {
		return Message{ID: m.ID, Body: []byte("malformed")}
	}
	return Message{ID: m.ID, Body: []byte(raw)}
}
func (t *RedisTransport) Ack(ctx context.Context, id string) error {
	n, err := t.client.XAck(ctx, t.stream, Group, id).Result()
	if err != nil || n != 1 {
		return ErrDelivery
	}
	return nil
}
