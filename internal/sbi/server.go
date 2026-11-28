package sbi

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ServerConfig is the deployment SBI listener configuration.
type ServerConfig struct {
	ListenAddress string
	TLSCertFile   string
	TLSKeyFile    string
	Handler       http.Handler
}

// Server is an HTTP/2 SBI listener. Plaintext uses h2c with prior knowledge;
// TLS uses HTTP/2 ALPN. It has no HTTP/1.1 fallback for the plaintext path.
type Server struct {
	config   ServerConfig
	server   *http.Server
	listener net.Listener
	done     chan error
	once     sync.Once
}

func NewServer(config ServerConfig) *Server {
	return &Server{config: config, done: make(chan error, 1)}
}

// Start begins serving and returns the bound address.
func (s *Server) Start() (string, error) {
	listener, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return "", err
	}
	s.listener = listener
	s.server = &http.Server{Handler: s.config.Handler}
	useTLS := s.config.TLSCertFile != "" || s.config.TLSKeyFile != ""
	if useTLS {
		if s.config.TLSCertFile == "" || s.config.TLSKeyFile == "" {
			listener.Close()
			return "", errors.New("both TLS certFile and keyFile are required")
		}
		s.server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		go func() {
			s.done <- s.server.ServeTLS(listener, s.config.TLSCertFile, s.config.TLSKeyFile)
		}()
	} else {
		h2cHandler := h2c.NewHandler(s.config.Handler, &http2.Server{})
		s.server.Handler = h2cHandler
		go func() {
			s.done <- s.server.Serve(listener)
		}()
	}
	return listener.Addr().String(), nil
}

// Shutdown gracefully stops the listener within ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	err := s.server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close force-closes the listener.
func (s *Server) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// Wait returns the Serve result. It is used by tests to assert listener
// termination.
func (s *Server) Wait() error {
	return <-s.done
}
