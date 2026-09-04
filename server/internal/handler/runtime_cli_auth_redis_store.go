package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cliAuthKeyPrefix          = "mul:" + runtimePendingRedisHashTag + ":cli_auth:req:"
	cliAuthPendingPrefix      = "mul:" + runtimePendingRedisHashTag + ":cli_auth:pending:"
	cliAuthRedisPopMaxRetries = 5
)

func cliAuthKey(id string) string               { return cliAuthKeyPrefix + id }
func cliAuthPendingKey(runtimeID string) string { return cliAuthPendingPrefix + runtimeID }

type RedisCliAuthStore struct {
	rdb *redis.Client
}

func NewRedisCliAuthStore(rdb *redis.Client) *RedisCliAuthStore {
	return &RedisCliAuthStore{rdb: rdb}
}

func (s *RedisCliAuthStore) Create(ctx context.Context, runtimeID, action string) (*CliAuthRequest, error) {
	now := time.Now()
	req := &CliAuthRequest{
		ID: randomID(), RuntimeID: runtimeID, Action: action, Status: CliAuthPending,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(cliAuthRequestTTL),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal CLI auth request: %w", err)
	}
	pendingKey := cliAuthPendingKey(runtimeID)
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, cliAuthKey(req.ID), data, cliAuthStoreRetention)
	pipe.ZAdd(ctx, pendingKey, redis.Z{Score: float64(now.UnixNano()), Member: req.ID})
	pipe.Expire(ctx, pendingKey, cliAuthStoreRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.rdb.Del(ctx, cliAuthKey(req.ID)).Err()
		_ = s.rdb.ZRem(ctx, pendingKey, req.ID).Err()
		return nil, fmt.Errorf("persist CLI auth request: %w", err)
	}
	return req, nil
}

func (s *RedisCliAuthStore) Get(ctx context.Context, id string) (*CliAuthRequest, error) {
	return s.load(ctx, id)
}

func (s *RedisCliAuthStore) load(ctx context.Context, id string) (*CliAuthRequest, error) {
	raw, err := s.rdb.Get(ctx, cliAuthKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get CLI auth request: %w", err)
	}
	var req CliAuthRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode CLI auth request: %w", err)
	}
	if applyCliAuthTimeout(&req, time.Now()) {
		if err := s.persist(ctx, &req); err != nil {
			return nil, err
		}
		_ = s.rdb.ZRem(ctx, cliAuthPendingKey(req.RuntimeID), req.ID).Err()
	}
	return &req, nil
}

func (s *RedisCliAuthStore) persist(ctx context.Context, req *CliAuthRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal CLI auth request: %w", err)
	}
	if err := s.rdb.Set(ctx, cliAuthKey(req.ID), data, cliAuthStoreRetention).Err(); err != nil {
		return fmt.Errorf("persist CLI auth request: %w", err)
	}
	return nil
}

func (s *RedisCliAuthStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	count, err := s.rdb.ZCard(ctx, cliAuthPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending CLI auth: %w", err)
	}
	return count > 0, nil
}

func (s *RedisCliAuthStore) PopPending(ctx context.Context, runtimeID string) (*CliAuthRequest, error) {
	pendingKey := cliAuthPendingKey(runtimeID)
	for attempt := 0; attempt < cliAuthRedisPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending CLI auth: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		req, err := s.load(ctx, ids[0])
		if err != nil {
			return nil, err
		}
		if req == nil || req.Status != CliAuthPending {
			_ = s.rdb.ZRem(ctx, pendingKey, ids[0]).Err()
			continue
		}
		now := time.Now()
		req.Status = CliAuthRunning
		req.RunStartedAt = &now
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal claimed CLI auth request: %w", err)
		}
		claimed, err := claimPendingScript.Run(ctx, s.rdb,
			[]string{pendingKey, cliAuthKey(req.ID)}, req.ID, data,
			int(cliAuthStoreRetention.Seconds())).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending CLI auth: %w", err)
		}
		if claimed == 1 {
			return req, nil
		}
	}
	return nil, nil
}

func (s *RedisCliAuthStore) update(ctx context.Context, id string, fn func(*CliAuthRequest)) error {
	req, err := s.load(ctx, id)
	if err != nil || req == nil || cliAuthRequestTerminal(req.Status) {
		return err
	}
	fn(req)
	req.UpdatedAt = time.Now()
	return s.persist(ctx, req)
}

func (s *RedisCliAuthStore) Progress(ctx context.Context, id, verificationURL, userCode string) error {
	return s.update(ctx, id, func(req *CliAuthRequest) {
		req.Status = CliAuthRunning
		if verificationURL != "" {
			req.VerificationURL = verificationURL
		}
		if userCode != "" {
			req.UserCode = userCode
		}
	})
}

func (s *RedisCliAuthStore) Complete(ctx context.Context, id string, authenticated bool) error {
	return s.update(ctx, id, func(req *CliAuthRequest) {
		req.Status = CliAuthCompleted
		req.Authenticated = &authenticated
	})
}

func (s *RedisCliAuthStore) Fail(ctx context.Context, id, errMsg string) error {
	return s.update(ctx, id, func(req *CliAuthRequest) {
		req.Status = CliAuthFailed
		req.Error = errMsg
	})
}
