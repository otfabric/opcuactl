package conn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/otfabric/opcua"
	"github.com/otfabric/opcua/ua"
)

// Config holds the connection parameters extracted from CLI flags.
type Config struct {
	Endpoint           string
	SecurityPolicy     string
	SecurityMode       string
	Auth               string
	Username           string
	Password           string
	CertFile           string
	KeyFile            string
	ServerCertFile     string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

// NewClient creates a connected OPC UA client from the config.
func (cfg *Config) NewClient(ctx context.Context) (*opcua.Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("--endpoint is required (or set OPCUACTL_ENDPOINT)")
	}

	opts := cfg.buildOptions()

	c, err := opcua.NewClient(cfg.Endpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}

	connectCtx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if err := c.Connect(connectCtx); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", cfg.Endpoint, err)
	}

	return c, nil
}

func (cfg *Config) buildOptions() []opcua.Option {
	var opts []opcua.Option

	if cfg.SecurityPolicy != "" && !strings.EqualFold(cfg.SecurityPolicy, "None") {
		opts = append(opts, opcua.SecurityPolicy(cfg.SecurityPolicy))
	}

	if cfg.SecurityMode != "" {
		mode := ua.MessageSecurityModeFromString(cfg.SecurityMode)
		if mode != ua.MessageSecurityModeInvalid {
			opts = append(opts, opcua.SecurityMode(mode))
		}
	}

	switch strings.ToLower(cfg.Auth) {
	case "username":
		opts = append(opts, opcua.AuthUsername(cfg.Username, cfg.Password))
	case "certificate":
		if cfg.CertFile != "" {
			opts = append(opts, opcua.CertificateFile(cfg.CertFile))
		}
		if cfg.KeyFile != "" {
			opts = append(opts, opcua.PrivateKeyFile(cfg.KeyFile))
		}
	default:
		opts = append(opts, opcua.AuthAnonymous())
	}

	if cfg.CertFile != "" && !strings.EqualFold(cfg.Auth, "certificate") {
		opts = append(opts, opcua.CertificateFile(cfg.CertFile))
	}
	if cfg.KeyFile != "" && !strings.EqualFold(cfg.Auth, "certificate") {
		opts = append(opts, opcua.PrivateKeyFile(cfg.KeyFile))
	}
	if cfg.ServerCertFile != "" {
		opts = append(opts, opcua.RemoteCertificateFile(cfg.ServerCertFile))
	}
	if cfg.InsecureSkipVerify {
		opts = append(opts, opcua.InsecureSkipVerify())
	}

	return opts
}

// TimeoutContext returns a context with the configured timeout, or the parent context.
func (cfg *Config) TimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if cfg.Timeout > 0 {
		return context.WithTimeout(ctx, cfg.Timeout)
	}
	return ctx, func() {}
}
