package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/deployhub-io/deployhub/internal/model"
	"github.com/redis/go-redis/v9"
)

const (
	PendingKey    = "deployhub:jobs:pending"
	ProcessingKey = "deployhub:jobs:processing"
)

type Queue struct {
	client *redis.Client
}

func New(addr, password string) *Queue {
	return &Queue{client: redis.NewClient(&redis.Options{Addr: addr, Password: password})}
}

func (q *Queue) Client() *redis.Client { return q.client }
func (q *Queue) Close() error          { return q.client.Close() }

func (q *Queue) Ping(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		if err := q.client.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("connect to redis: %w", lastErr)
}

func (q *Queue) Enqueue(ctx context.Context, job model.DeployJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode job: %w", err)
	}
	return q.client.LPush(ctx, PendingKey, payload).Err()
}

func (q *Queue) Claim(ctx context.Context, timeout time.Duration) (model.DeployJob, string, error) {
	payload, err := q.client.BRPopLPush(ctx, PendingKey, ProcessingKey, timeout).Result()
	if err != nil {
		return model.DeployJob{}, "", err
	}
	var job model.DeployJob
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		return model.DeployJob{}, payload, fmt.Errorf("decode job: %w", err)
	}
	return job, payload, nil
}

func (q *Queue) Ack(ctx context.Context, payload string) error {
	return q.client.LRem(ctx, ProcessingKey, 1, payload).Err()
}
