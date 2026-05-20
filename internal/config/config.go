package config

import (
	"strings"
	"time"

	"github.com/chilly266futon/exchange-shared/pkg/config"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Config struct {
	config.BaseConfig
	SpotClient  SpotClient  `mapstructure:"spot_client"`
	OrderLimits OrderLimits `mapstructure:"order_limits"`
}

type SpotClient struct {
	Addr       string        `mapstructure:"addr"`
	Timeout    time.Duration `mapstructure:"timeout"`
	TLSEnabled bool          `mapstructure:"tls_enabled"`
	CAFile     string        `mapstructure:"ca_file"`
}

// OrderLimits — максимальное количество активных ордеров по роли.
// Ключ — имя роли (COMMON, VERIFIED, PREMIUM, ADMIN).
type OrderLimits struct {
	MaxActiveOrders map[string]int `mapstructure:"max_active_orders"`
	Default         int            `mapstructure:"default"`
}

func Load(path string, l *zap.Logger) *Config {
	base := config.LoadBase(path, "ORDER", l)

	viper.SetConfigFile(path)
	viper.SetEnvPrefix("ORDER")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		l.Fatal("failed to read config", zap.Error(err))
	}

	cfg := Config{
		BaseConfig: *base,
	}

	// Unmarshal только order-специфичных секций
	if sub := viper.Sub("spot_client"); sub != nil {
		if err := sub.Unmarshal(&cfg.SpotClient); err != nil {
			l.Fatal("failed to unmarshal spot_client", zap.Error(err))
		}
	}
	if sub := viper.Sub("order_limits"); sub != nil {
		if err := sub.Unmarshal(&cfg.OrderLimits); err != nil {
			l.Fatal("failed to unmarshal order_limits", zap.Error(err))
		}
	}

	// env overrides
	if v := viper.GetString("spot_client.addr"); v != "" {
		cfg.SpotClient.Addr = v
	}
	if viper.IsSet("spot_client.tls_enabled") {
		cfg.SpotClient.TLSEnabled = viper.GetBool("spot_client.tls_enabled")
	}
	if v := viper.GetString("spot_client.ca_file"); v != "" {
		cfg.SpotClient.CAFile = v
	}

	// defaults
	if cfg.SpotClient.Addr == "" {
		cfg.SpotClient.Addr = "localhost:50052"
	}
	if cfg.SpotClient.Timeout == 0 {
		cfg.SpotClient.Timeout = 10 * time.Second
	}
	if !cfg.SpotClient.TLSEnabled {
		cfg.SpotClient.TLSEnabled = true // по умолчанию включен TLS
	}
	if cfg.SpotClient.CAFile == "" {
		cfg.SpotClient.CAFile = "certs/ca.crt"
	}

	if cfg.OrderLimits.Default == 0 {
		cfg.OrderLimits.Default = 5
	}
	if cfg.OrderLimits.MaxActiveOrders == nil {
		cfg.OrderLimits.MaxActiveOrders = map[string]int{
			"COMMON":   5,
			"VERIFIED": 20,
			"PREMIUM":  100,
			"ADMIN":    1000,
		}
	}

	return &cfg
}
