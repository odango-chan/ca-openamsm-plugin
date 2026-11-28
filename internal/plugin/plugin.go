// Package plugin implements the OpenAMSM Event/Delivery Plugin lifecycle and the
// bridge between the IPC process protocol, SBI ingress and Delivery.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/odango-chan/ca-openamsm-plugin/internal/delivery"
	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
	"github.com/odango-chan/ca-openamsm-plugin/internal/sbi"
	"github.com/odango-chan/ca-openamsm-plugin/internal/staging"
)

// maxFrameBytes is the hard per-frame stdin limit.
const maxFrameBytes = 8 << 20

// Options carries the process streams. Tests inject pipes; production uses the
// standard streams.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Plugin is one long-lived Plugin process.
type Plugin struct {
	manifest   Manifest
	classifier event.Classifier
	options    Options

	reader *ipc.Reader
	writer *ipc.Writer
	logger *log.Logger

	config   Config
	store    *staging.Store
	registry *delivery.Registry
	server   *sbi.Server

	initialized         bool
	initializeMessageID string
	seenHostMessages    map[string]struct{}

	idMu  sync.Mutex
	idSeq int

	shuttingDown atomic.Bool
	inFlight     sync.WaitGroup
}

// New builds a Plugin. classifier must be BlockedClassifier in production.
func New(manifest Manifest, classifier event.Classifier, options Options) *Plugin {
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	return &Plugin{
		manifest:         manifest,
		classifier:       classifier,
		options:          options,
		logger:           log.New(options.Stderr, "ca-openamsm-plugin: ", log.LstdFlags),
		registry:         delivery.NewRegistry(),
		seenHostMessages: map[string]struct{}{},
	}
}

// Run reads IPC frames until host.shutdown or a protocol violation.
func (p *Plugin) Run() error {
	p.reader = ipc.NewReader(p.options.Stdin, maxFrameBytes)
	p.writer = ipc.NewWriter(p.options.Stdout)
	for {
		frame, err := p.reader.Next()
		if errors.Is(err, io.EOF) {
			return &ipc.Error{Code: "EOF_WITHOUT_SHUTDOWN"}
		}
		if err != nil {
			return err
		}
		if _, duplicate := p.seenHostMessages[frame.MessageID]; duplicate {
			return p.violation(ipc.CodeDuplicateMessageID, frame.MessageID)
		}
		p.seenHostMessages[frame.MessageID] = struct{}{}
		if !p.initialized && frame.Type != "host.initialize" {
			return p.violation(ipc.CodeLifecycleViolation, frame.Type)
		}

		switch frame.Type {
		case "host.initialize":
			if err := p.handleInitialize(frame); err != nil {
				return err
			}
		case "ingress.ack":
			if err := p.handleAck(frame); err != nil {
				return err
			}
		case "delivery.dispatch":
			if err := p.handleDispatch(frame); err != nil {
				return err
			}
		case "host.cancel":
			if err := p.handleCancel(frame); err != nil {
				return err
			}
		case "host.shutdown":
			return p.handleShutdown(frame)
		default:
			return p.violation(ipc.CodeUnknownMessageType, frame.Type)
		}
	}
}

type initializePayload struct {
	PluginID        string          `json:"pluginId"`
	ExpectedVersion string          `json:"expectedVersion"`
	IPCVersion      string          `json:"ipcVersion"`
	Config          json.RawMessage `json:"config"`
	ArtifactStaging *struct {
		InboundDirectory  string `json:"inboundDirectory"`
		OutboundDirectory string `json:"outboundDirectory"`
	} `json:"artifactStaging"`
}

type readyPayload struct {
	PluginID      string   `json:"pluginId"`
	PluginVersion string   `json:"pluginVersion"`
	IPCVersion    string   `json:"ipcVersion"`
	Extensions    []string `json:"extensions"`
}

func (p *Plugin) handleInitialize(frame ipc.Frame) error {
	if p.initialized {
		return p.violation("DUPLICATE_INITIALIZE", frame.Type)
	}
	if frame.CorrelationID != "" {
		return p.violation(ipc.CodeUnexpectedCorrelation, frame.Type)
	}
	payload, err := ipc.DecodePayload[initializePayload](frame.Payload)
	if err != nil {
		return err
	}
	if payload.PluginID != p.manifest.PluginID ||
		payload.ExpectedVersion != p.manifest.Version ||
		payload.IPCVersion != ipc.APIVersion {
		return p.violation("INITIALIZE_IDENTITY_MISMATCH", frame.Type)
	}
	config, err := parseConfig(payload.Config)
	if err != nil {
		return err
	}
	if payload.ArtifactStaging == nil ||
		payload.ArtifactStaging.InboundDirectory == "" ||
		payload.ArtifactStaging.OutboundDirectory == "" {
		return p.violation("MISSING_ARTIFACT_STAGING", frame.Type)
	}
	store, err := staging.New(payload.ArtifactStaging.InboundDirectory, payload.ArtifactStaging.OutboundDirectory)
	if err != nil {
		return p.violation("INVALID_ARTIFACT_STAGING", err.Error())
	}
	server := sbi.NewServer(sbi.ServerConfig{
		ListenAddress: config.SBI.ListenAddress,
		TLSCertFile:   config.SBI.TLSCertFile,
		TLSKeyFile:    config.SBI.TLSKeyFile,
		Handler:       http.HandlerFunc(p.handleIngress),
	})
	address, err := server.Start()
	if err != nil {
		return &ipc.Error{Code: "SBI_LISTEN_FAILED", Detail: err.Error(), Err: err}
	}
	p.config = config
	p.store = store
	p.server = server
	p.initializeMessageID = frame.MessageID
	p.initialized = true
	p.logger.Printf("SBI_LISTENING address=%s peerId=%s", address, config.PeerID)

	extensions := append([]string(nil), p.manifest.Extensions...)
	sort.Strings(extensions)
	return p.writer.Send(p.nextMessageID("m"), frame.MessageID, "plugin.ready", readyPayload{
		PluginID:      p.manifest.PluginID,
		PluginVersion: p.manifest.Version,
		IPCVersion:    ipc.APIVersion,
		Extensions:    extensions,
	})
}

func (p *Plugin) nextMessageID(prefix string) string {
	p.idMu.Lock()
	defer p.idMu.Unlock()
	p.idSeq++
	return fmt.Sprintf("%s%d-%s", prefix, p.idSeq, randomSuffix())
}

func (p *Plugin) violation(code, detail string) *ipc.Error {
	p.logger.Printf("PROTOCOL_VIOLATION code=%s detail=%s", code, safeDetail(detail))
	return &ipc.Error{Code: code, Detail: detail}
}

func safeDetail(value string) string {
	if len(value) > 256 {
		return value[:256]
	}
	return value
}

func (p *Plugin) shutdownContext() (context.Context, context.CancelFunc) {
	grace := time.Duration(p.config.shutdownGrace()) * time.Millisecond
	if grace <= 0 {
		grace = 5 * time.Second
	}
	return context.WithTimeout(context.Background(), grace)
}
