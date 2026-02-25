package config

import (
	"os"
	"time"

	"github.com/chilly266futon/exchange-shared/pkg/logger"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	SpotService SpotServiceConfig `yaml:"spot_service"`
	RateLimit   RateLimitConfig   `yaml:"rate_limit"`
	Health      HealthConfig      `yaml:"health"`
	Logger      logger.Config     `yaml:"logger"`
}

type ServerConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type SpotServiceConfig struct {
	Addr          string        `yaml:"addr"`
	Timeout       time.Duration `yaml:"timeout"`
	EnableBreaker bool          `yaml:"enable_breaker"`
	Breaker       BreakerConfig `yaml:"breaker"`
}

type BreakerConfig struct {
	MaxRequests uint32        `yaml:"max_requests"`
	Interval    time.Duration `yaml:"interval"`
	Timeout     time.Duration `yaml:"timeout"`
	Attempts    uint32        `yaml:"attempts"`
}

// RateLimitConfig конфигурация rate limiting
type RateLimitConfig struct {
	Enabled           bool                             `yaml:"enabled"`
	RequestsPerSecond float64                          `yaml:"requests_per_second"`
	Burst             int                              `yaml:"burst"`
	Methods           map[string]MethodRateLimitConfig `yaml:"methods"`
	PerUser           PerUserLimit                     `yaml:"per_user"`
}

type HealthConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// MethodRateLimitConfig лимит для конкретного метода
type MethodRateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

type PerUserLimit struct {
	OrdersPerMinute int `yaml:"orders_per_minute"`
	Burst           int `yaml:"burst"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// MustLoad загружает конфигурацию или паникует
func MustLoad(configPath string) *Config {
	cfg, err := Load(configPath)
	if err != nil {
		panic(err)
	}
	return cfg
}
