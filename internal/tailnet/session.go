package tailnet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// Hostname returns this machine's MagicDNS name on the tailnet, such as
// "machine.tailnet.ts.net", or an error when Tailscale is not connected.
func Hostname(ctx context.Context, client *local.Client) (string, error) {
	tailscaleStatus, err := client.StatusWithoutPeers(ctx)
	if err != nil {
		return "", fmt.Errorf("Tailscale is not reachable: %w", err)
	}
	if tailscaleStatus.BackendState != ipn.Running.String() {
		return "", fmt.Errorf("Tailscale is %s", strings.ToLower(tailscaleStatus.BackendState))
	}
	if tailscaleStatus.Self == nil || tailscaleStatus.Self.DNSName == "" {
		return "", errors.New("Tailscale has no MagicDNS name for this machine")
	}
	return strings.TrimSuffix(tailscaleStatus.Self.DNSName, "."), nil
}

// Session is an open connection to the Tailscale daemon. Mappings stored under
// its ID live only as long as the connection: Tailscale deletes them when it closes.
type Session struct {
	ID      string
	watcher *local.IPNBusWatcher
	closed  chan struct{}
}

// OpenSession connects to the Tailscale daemon; the session ends when ctx is done or Close is called.
func OpenSession(ctx context.Context, client *local.Client) (*Session, error) {
	watcher, err := client.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		return nil, fmt.Errorf("opening a Tailscale session: %w", err)
	}
	notification, err := watcher.Next()
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("opening a Tailscale session: %w", err)
	}
	if notification.SessionID == "" {
		watcher.Close()
		return nil, errors.New("opening a Tailscale session: Tailscale sent no session ID")
	}

	session := &Session{ID: notification.SessionID, watcher: watcher, closed: make(chan struct{})}
	go func() {
		defer close(session.closed)
		// Reading keeps the connection flowing; an error means Tailscale ended the session.
		for {
			if _, err := watcher.Next(); err != nil {
				return
			}
		}
	}()
	return session, nil
}

// Closed is closed once the session has ended, together with its mappings.
func (s *Session) Closed() <-chan struct{} {
	return s.closed
}

// Close ends the session, which removes its mappings from the tailnet.
func (s *Session) Close() error {
	return s.watcher.Close()
}

// Mappings is what a Session serves: one HTTPS mapping per port, plus the Dashboard.
type Mappings struct {
	// Ports are served at the same port number on the tailnet, over HTTPS.
	Ports []uint16
	// DashboardTarget is the local URL of the Dashboard, served over HTTPS on DashboardPort; empty for none.
	DashboardTarget string
	DashboardPort   uint16
	// DashboardRedirectPort redirects plain HTTP to the Dashboard, keeping the path; 0 for no redirect.
	DashboardRedirectPort uint16
}

// DashboardURL is the address of the Dashboard served on port of hostname, without a trailing slash.
func DashboardURL(hostname string, port uint16) string {
	if port == 443 {
		return "https://" + hostname
	}
	return fmt.Sprintf("https://%s:%d", hostname, port)
}

// Apply replaces the mappings of the session inside serveConfig, which must
// come from GetServeConfig so its ETag rejects the write if someone changed it meanwhile.
func (s *Session) Apply(ctx context.Context, client *local.Client, serveConfig *ipn.ServeConfig, hostname string, mappings Mappings) error {
	sessionConfig := new(ipn.ServeConfig)
	for _, port := range mappings.Ports {
		proxy := &ipn.HTTPHandler{Proxy: fmt.Sprintf("http://localhost:%d", port)}
		sessionConfig.SetWebHandler(proxy, hostname, port, "/", true, "")
	}
	if mappings.DashboardTarget != "" {
		proxy := &ipn.HTTPHandler{Proxy: mappings.DashboardTarget}
		sessionConfig.SetWebHandler(proxy, hostname, mappings.DashboardPort, "/", true, "")
		if mappings.DashboardRedirectPort != 0 {
			redirect := &ipn.HTTPHandler{Redirect: DashboardURL(hostname, mappings.DashboardPort) + "${REQUEST_URI}"}
			sessionConfig.SetWebHandler(redirect, hostname, mappings.DashboardRedirectPort, "/", false, "")
		}
	}

	isEmpty := len(sessionConfig.TCP) == 0
	current, hasCurrent := serveConfig.Foreground[s.ID]
	if isEmpty && !hasCurrent {
		return nil
	}
	if hasCurrent && !isEmpty && sameConfig(current, sessionConfig) {
		return nil
	}

	if isEmpty {
		delete(serveConfig.Foreground, s.ID)
	} else {
		if serveConfig.Foreground == nil {
			serveConfig.Foreground = map[string]*ipn.ServeConfig{}
		}
		serveConfig.Foreground[s.ID] = sessionConfig
	}
	if err := client.SetServeConfig(ctx, serveConfig); err != nil {
		return fmt.Errorf("updating the Tailscale Serve config: %w", err)
	}
	return nil
}

// sameConfig compares two configs by their JSON, which ignores the difference
// between empty and missing maps that a round trip through Tailscale introduces.
func sameConfig(a, b *ipn.ServeConfig) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && bytes.Equal(aJSON, bJSON)
}
