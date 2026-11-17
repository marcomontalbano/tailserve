// Package control lets other tailserve commands talk to the running one over a Unix socket.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/marcomontalbano/tailserve/internal/status"
)

// ErrNotRunning means no tailserve is listening on the socket.
var ErrNotRunning = errors.New("tailserve is not running")

// ErrAlreadyRunning means another tailserve already owns the socket.
var ErrAlreadyRunning = errors.New("tailserve is already running")

// SocketPath returns the socket of the current user's tailserve, inside the per-user temporary directory.
func SocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("tailserve-%d.sock", os.Getuid()))
}

// Listen claims the socket, replacing one left behind by a tailserve that crashed.
func Listen() (net.Listener, error) {
	path := SocketPath()
	if connection, err := net.DialTimeout("unix", path, time.Second); err == nil {
		connection.Close()
		return nil, ErrAlreadyRunning
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("removing the stale socket %s: %w", path, err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", path, err)
	}
	return listener, nil
}

// Handler answers GET /status with the current Snapshot and POST /stop by calling stop.
func Handler(currentSnapshot func() status.Snapshot, stop func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(currentSnapshot())
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		stop()
	})
	return mux
}

// FetchSnapshot asks the running tailserve for its Snapshot, or returns ErrNotRunning.
func FetchSnapshot(ctx context.Context) (status.Snapshot, error) {
	var snapshot status.Snapshot
	response, err := send(ctx, http.MethodGet, "/status")
	if err != nil {
		return snapshot, err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("reading the status of the running tailserve: %w", err)
	}
	return snapshot, nil
}

// RequestStop asks the running tailserve to stop and waits until it has released the socket.
func RequestStop(ctx context.Context) error {
	response, err := send(ctx, http.MethodPost, "/stop")
	if err != nil {
		return err
	}
	response.Body.Close()

	for {
		if _, err := os.Stat(SocketPath()); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for tailserve to stop: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func send(ctx context.Context, method, path string) (*http.Response, error) {
	client := http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", SocketPath())
		},
	}}
	request, err := http.NewRequestWithContext(ctx, method, "http://tailserve"+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		var dialError *net.OpError
		if errors.As(err, &dialError) && dialError.Op == "dial" {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("talking to the running tailserve: %w", err)
	}
	if response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("talking to the running tailserve: %s", response.Status)
	}
	return response, nil
}
