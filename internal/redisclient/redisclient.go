package redisclient

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config Redis 连接配置（直接用 xiaozhi-server-go 自己的 config.Redis）。
type Config struct {
	Addr     string
	Password string
	DB       int
}

// New 创建 Redis 客户端 + ping 验证。
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*redis.Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping %s: %w", cfg.Addr, err)
	}
	logger.Info("redis connected", "addr", cfg.Addr, "db", cfg.DB)
	return client, nil
}