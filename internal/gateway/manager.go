package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Running bool   `json:"running"`
	Address string `json:"address"`
	Error   string `json:"error,omitempty"`
}

type Manager struct {
	store    *Store
	selector *Selector
	build    *BuildClient

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	status   Status
}

func NewManager(store *Store) *Manager {
	return &Manager{store: store, selector: NewSelector(store), build: NewBuildClient()}
}

func (m *Manager) Start(address string) error {
	address, err := validateListenAddress(address)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		return fmt.Errorf("gateway is already running on %s", m.status.Address)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		m.status = Status{Error: err.Error()}
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	server := &http.Server{
		Handler:           m.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	m.listener = listener
	m.server = server
	m.status = Status{Running: true, Address: listener.Addr().String()}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			m.mu.Lock()
			m.status.Running = false
			m.status.Error = serveErr.Error()
			m.server = nil
			m.listener = nil
			m.mu.Unlock()
		}
	}()
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	server := m.server
	m.mu.Unlock()
	if server == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	err := server.Shutdown(ctx)
	m.mu.Lock()
	m.server = nil
	m.listener = nil
	m.status.Running = false
	if err != nil {
		m.status.Error = err.Error()
	} else {
		m.status.Error = ""
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func validateListenAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("gateway listen address is required")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid gateway listen address %q: %w", address, err)
	}
	if host == "" {
		return "", fmt.Errorf("gateway listen host is required")
	}
	if ip := net.ParseIP(host); ip == nil && !strings.EqualFold(host, "localhost") {
		return "", fmt.Errorf("gateway listen host must be an IP address or localhost")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return "", fmt.Errorf("gateway listen port must be between 0 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
