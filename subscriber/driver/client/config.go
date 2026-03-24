package client

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

const (
	DefaultEnvNATSPrefix = "NATS"

	DefaultTimeout = time.Millisecond * 100
)

type Config struct {
	Addr                 string        `env:"_ADDR" envDefault:"nats://127.0.0.1:4222"`
	Username             string        `env:"_USERNAME" envDefault:""`
	Password             string        `env:"_PASSWORD" envDefault:""`
	Seed                 string        `env:"_SEED" envDefault:""`
	DrainTimeout         time.Duration `env:"_DRAIN_TIMEOUT" envDefault:"1s"`
	MaxReconnects        int           `env:"_MAX_RECONNECTS" envDefault:"-1"`
	ReconnectWait        time.Duration `env:"_RECONNECT_WAIT" envDefault:"1s"`
	RetryOnFailedConnect bool          `env:"_RETRY_ON_FAILED_CONNECT" envDefault:"true"`
	ConcurrentSizeVal    int           `env:"_CONCURRENT_SIZE" envDefault:"20"`
	MaxConcurrentSize    uint64        `env:"_MAX_CONCURRENT_SIZE" envDefault:"100"`
	ReadTimeoutVal       time.Duration `env:"_READ_TIMEOUT" envDefault:"30s"`
}

func NATSConfigFromEnv(prefixes ...string) (*Config, error) {
	c := new(Config)

	prefix := DefaultEnvNATSPrefix
	if len(prefixes) > 0 {
		prefix = prefixes[0]
	}

	err := env.ParseWithOptions(c, env.Options{
		Prefix: strings.ToUpper(prefix),
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (cfg *Config) ConcurrentSize() int {
	if cfg.ConcurrentSizeVal <= 0 {
		return runtime.NumCPU()
	}

	return cfg.ConcurrentSizeVal
}

func (cfg *Config) ReadTimeout() time.Duration {
	if cfg.ReadTimeoutVal <= 0 {
		return DefaultTimeout
	}

	return cfg.ReadTimeoutVal
}

func (cfg *Config) Options() ([]nats.Option, error) {
	options := []nats.Option{
		nats.RetryOnFailedConnect(cfg.RetryOnFailedConnect),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
	}

	if cfg.Username != "" && cfg.Password != "" {
		options = append(options, nats.UserInfo(cfg.Username, cfg.Password))
	}

	if cfg.DrainTimeout > 0 {
		options = append(options, nats.DrainTimeout(cfg.DrainTimeout))
	}

	if cfg.Seed != "" {
		kp, err := nkeys.FromSeed([]byte(cfg.Seed))
		if err != nil {
			return nil, fmt.Errorf("key from seed: %w", err)
		}

		usrNKey, err := kp.PublicKey()
		if err != nil {
			return nil, fmt.Errorf("public key from seed: %w", err)
		}

		options = append(options, nats.Nkey(usrNKey, func(nonce []byte) ([]byte, error) {
			return kp.Sign(nonce)
		}))
	}

	options = append(options, nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
		// Error handler uses package-level slog since it's a NATS callback
		// with no access to the client's logger.
		logNATSError(nc, sub, err)
	}))

	return options, nil
}
