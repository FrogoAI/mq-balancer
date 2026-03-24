package client

import (
	"runtime"
	"testing"
	"time"

	"github.com/nats-io/nkeys"

	"github.com/FrogoAI/testutils"
)

func TestNATSConfigFromEnv(t *testing.T) {
	t.Setenv("CUSTOM_NATS_ADDR", "test")

	c, err := NATSConfigFromEnv("custom_nats")
	testutils.Equal(t, err, nil)
	testutils.Equal(t, c.Addr, "test")

	_, err = NATSConfigFromEnv("unknown_nats")
	testutils.Equal(t, err, nil)
}

func TestNATSConfigFromEnv_DefaultPrefix(t *testing.T) {
	t.Setenv("NATS_ADDR", "nats://default:4222")

	c, err := NATSConfigFromEnv()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, c.Addr, "nats://default:4222")
}

func TestConfig_ConcurrentSize(t *testing.T) {
	cases := []struct {
		name string
		val  int
		want int
	}{
		{name: "positive", val: 5, want: 5},
		{name: "zero", val: 0, want: runtime.NumCPU()},
		{name: "negative", val: -1, want: runtime.NumCPU()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{ConcurrentSizeVal: tc.val}
			testutils.Equal(t, cfg.ConcurrentSize(), tc.want)
		})
	}
}

func TestConfig_ReadTimeout(t *testing.T) {
	cases := []struct {
		name string
		val  time.Duration
		want time.Duration
	}{
		{name: "positive", val: 5 * time.Second, want: 5 * time.Second},
		{name: "zero", val: 0, want: DefaultTimeout},
		{name: "negative", val: -1, want: DefaultTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{ReadTimeoutVal: tc.val}
			testutils.Equal(t, cfg.ReadTimeout(), tc.want)
		})
	}
}

func TestConfig_Options_Basic(t *testing.T) {
	cfg := &Config{
		RetryOnFailedConnect: true,
		MaxReconnects:        3,
		ReconnectWait:        time.Second,
	}

	opts, err := cfg.Options()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(opts) > 0, true)
}

func TestConfig_Options_WithAuth(t *testing.T) {
	cfg := &Config{
		Username:      "user",
		Password:      "pass",
		DrainTimeout:  time.Second,
		MaxReconnects: 3,
		ReconnectWait: time.Second,
	}

	opts, err := cfg.Options()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(opts) > 0, true)
}

func TestConfig_Options_InvalidSeed(t *testing.T) {
	cfg := &Config{
		Seed:          "invalid-seed",
		MaxReconnects: 3,
		ReconnectWait: time.Second,
	}

	_, err := cfg.Options()
	testutils.Equal(t, err != nil, true)
}

func TestConfig_Options_ValidSeed(t *testing.T) {
	// Generate a valid nkey seed for testing
	kp, err := nkeys.CreateUser()
	testutils.Equal(t, err, nil)

	seed, err := kp.Seed()
	testutils.Equal(t, err, nil)

	cfg := &Config{
		Seed:          string(seed),
		MaxReconnects: 3,
		ReconnectWait: time.Second,
	}

	opts, err := cfg.Options()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(opts) > 0, true)
}

func TestConfig_Options_NoDrainTimeout(t *testing.T) {
	cfg := &Config{
		DrainTimeout:  0,
		MaxReconnects: 3,
		ReconnectWait: time.Second,
	}

	opts, err := cfg.Options()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(opts) > 0, true)
}
