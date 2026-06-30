package store

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kube-argus/kube-argus/service/internal/config"
)

// redisStore is the HA backend: TTL via SET EX, one-shot reads via GETDEL.
type redisStore struct {
	c *redis.Client
}

func newRedis(cfg config.StoreConfig) (Store, error) {
	opt := &redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}
	if cfg.RedisTLS {
		opt.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	c := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &redisStore{c: c}, nil
}

func (r *redisStore) SaveAuthRequest(ctx context.Context, key string, ar AuthRequest, ttl time.Duration) error {
	return r.save(ctx, "auth:"+key, ar, ttl)
}

func (r *redisStore) TakeAuthRequest(ctx context.Context, key string) (AuthRequest, error) {
	var ar AuthRequest
	err := r.take(ctx, "auth:"+key, &ar)
	return ar, err
}

func (r *redisStore) SaveCode(ctx context.Context, code string, g CodeGrant, ttl time.Duration) error {
	return r.save(ctx, "code:"+code, g, ttl)
}

func (r *redisStore) TakeCode(ctx context.Context, code string) (CodeGrant, error) {
	var g CodeGrant
	err := r.take(ctx, "code:"+code, &g)
	return g, err
}

func (r *redisStore) SaveRefresh(ctx context.Context, token string, g RefreshGrant, ttl time.Duration) error {
	return r.save(ctx, "refresh:"+token, g, ttl)
}

func (r *redisStore) TakeRefresh(ctx context.Context, token string) (RefreshGrant, error) {
	var g RefreshGrant
	err := r.take(ctx, "refresh:"+token, &g)
	return g, err
}

func (r *redisStore) Close() error { return r.c.Close() }

func (r *redisStore) save(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.c.Set(ctx, key, b, ttl).Err()
}

// take is one-shot: GETDEL returns and removes atomically (replay protection).
func (r *redisStore) take(ctx context.Context, key string, dst any) error {
	b, err := r.c.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
