package plugin

import (
	"encoding/json"
	"strings"

	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
)

// Config is the deployment configuration delivered in host.initialize. It is
// closed: unknown fields fail the handshake.
type Config struct {
	PeerID                string   `json:"peerId"`
	ExternalRequestHeader string   `json:"externalRequestHeader"`
	SBI                   SBI      `json:"sbi"`
	Timeouts              Timeouts `json:"timeouts"`
	Limits                Limits   `json:"limits"`
}

// SBI is the listener configuration.
type SBI struct {
	ListenAddress   string `json:"listenAddress"`
	MaxRequestBytes int64  `json:"maxRequestBytes"`
	TLSCertFile     string `json:"tlsCertFile"`
	TLSKeyFile      string `json:"tlsKeyFile"`
}

// Timeouts are millisecond bounds for module tests and deployment tuning.
type Timeouts struct {
	AckTimeoutMs      int64 `json:"ackTimeoutMs"`
	DeliveryTimeoutMs int64 `json:"deliveryTimeoutMs"`
	ShutdownGraceMs   int64 `json:"shutdownGraceMs"`
}

// Limits bound response buffering.
type Limits struct {
	MaxResponseBytes int64 `json:"maxResponseBytes"`
}

const (
	defaultExternalRequestHeader = "X-CA-Request-Id"
	defaultMaxRequestBytes       = 16 << 20
	defaultAckTimeoutMs          = 5000
	defaultDeliveryTimeoutMs     = 30000
	defaultShutdownGraceMs       = 5000
	defaultMaxResponseBytes      = 32 << 20
)

func parseConfig(raw json.RawMessage) (Config, error) {
	var config Config
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	decoded, err := ipc.DecodePayload[Config](raw)
	if err != nil {
		return Config{}, err
	}
	config = decoded
	if strings.TrimSpace(config.PeerID) == "" {
		return Config{}, &ipc.Error{Code: "CONFIG_PEER_ID_MISSING"}
	}
	if strings.TrimSpace(config.SBI.ListenAddress) == "" {
		return Config{}, &ipc.Error{Code: "CONFIG_LISTEN_ADDRESS_MISSING"}
	}
	if config.ExternalRequestHeader == "" {
		config.ExternalRequestHeader = defaultExternalRequestHeader
	}
	if config.SBI.MaxRequestBytes <= 0 {
		config.SBI.MaxRequestBytes = defaultMaxRequestBytes
	}
	if config.Timeouts.AckTimeoutMs <= 0 {
		config.Timeouts.AckTimeoutMs = defaultAckTimeoutMs
	}
	if config.Timeouts.DeliveryTimeoutMs <= 0 {
		config.Timeouts.DeliveryTimeoutMs = defaultDeliveryTimeoutMs
	}
	if config.Timeouts.ShutdownGraceMs <= 0 {
		config.Timeouts.ShutdownGraceMs = defaultShutdownGraceMs
	}
	if config.Limits.MaxResponseBytes <= 0 {
		config.Limits.MaxResponseBytes = defaultMaxResponseBytes
	}
	hasCert := config.SBI.TLSCertFile != ""
	hasKey := config.SBI.TLSKeyFile != ""
	if hasCert != hasKey {
		return Config{}, &ipc.Error{Code: "CONFIG_TLS_INCOMPLETE"}
	}
	return config, nil
}

func (c Config) ackTimeout() int64      { return c.Timeouts.AckTimeoutMs }
func (c Config) deliveryTimeout() int64 { return c.Timeouts.DeliveryTimeoutMs }
func (c Config) shutdownGrace() int64   { return c.Timeouts.ShutdownGraceMs }
