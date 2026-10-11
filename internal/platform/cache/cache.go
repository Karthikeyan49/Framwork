// Package cache is a small Redis adapter; Redis is never a source of truth.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct{ client *redis.Client }

func Open(raw string) (*Client, error) {
	options, err := redis.ParseURL(raw)
	if err != nil {
		return nil, errors.New("invalid Redis configuration")
	}
	options.DialTimeout = time.Second
	options.ReadTimeout = time.Second
	options.WriteTimeout = time.Second
	options.MaxRetries = -1
	options.ContextTimeoutEnabled = true
	return &Client{client: redis.NewClient(options)}, nil
}
func (c *Client) Ping(ctx context.Context) error {
	if c.client.Ping(ctx).Err() != nil {
		return errors.New("Redis unavailable")
	}
	return nil
}
func (c *Client) Close() error { return c.client.Close() }
