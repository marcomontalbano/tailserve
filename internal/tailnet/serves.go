// Package tailnet reads and changes what this machine serves through Tailscale Serve.
package tailnet

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// Serve is one Tailscale Serve mapping from a tailnet port to a local port.
type Serve struct {
	LocalPort   uint16
	TailnetPort uint16
	// URL is where the tailnet reaches the local port, such as "https://machine.tailnet.ts.net:5173".
	URL string
}

// ListServes returns every mapping to a local port, whoever created it.
func ListServes(ctx context.Context, client *local.Client) ([]Serve, error) {
	serveConfig, err := client.GetServeConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the Tailscale Serve config: %w", err)
	}
	return ServesOutsideSession(serveConfig, ""), nil
}

// ServesOutsideSession returns the mappings to a local port in serveConfig, both
// the persistent ones and those of every foreground session except sessionID.
func ServesOutsideSession(serveConfig *ipn.ServeConfig, sessionID string) []Serve {
	serves := servesOf(serveConfig)
	for foregroundSessionID, foregroundConfig := range serveConfig.Foreground {
		if foregroundSessionID != sessionID {
			serves = append(serves, servesOf(foregroundConfig)...)
		}
	}
	return serves
}

// TailnetPortsOutsideSession returns every tailnet port that serveConfig uses
// outside sessionID, including mappings to files, text or sockets that have no local port.
func TailnetPortsOutsideSession(serveConfig *ipn.ServeConfig, sessionID string) map[uint16]bool {
	usedPorts := map[uint16]bool{}
	for port := range serveConfig.TCP {
		usedPorts[port] = true
	}
	for foregroundSessionID, foregroundConfig := range serveConfig.Foreground {
		if foregroundSessionID == sessionID {
			continue
		}
		for port := range foregroundConfig.TCP {
			usedPorts[port] = true
		}
	}
	return usedPorts
}

// servesOf lists the mappings of one config, ignoring its foreground sessions.
func servesOf(serveConfig *ipn.ServeConfig) []Serve {
	var serves []Serve
	for hostPort, webConfig := range serveConfig.Web {
		_, tailnetPortText, err := net.SplitHostPort(string(hostPort))
		if err != nil {
			continue
		}
		tailnetPort, err := strconv.ParseUint(tailnetPortText, 10, 16)
		if err != nil {
			continue
		}
		scheme := "http"
		if handler := serveConfig.TCP[uint16(tailnetPort)]; handler != nil && handler.HTTPS {
			scheme = "https"
		}
		for mountPoint, handler := range webConfig.Handlers {
			localPort, ok := proxyPort(handler.Proxy)
			if !ok {
				continue
			}
			serves = append(serves, Serve{
				LocalPort:   localPort,
				TailnetPort: uint16(tailnetPort),
				URL:         scheme + "://" + string(hostPort) + strings.TrimSuffix(mountPoint, "/"),
			})
		}
	}

	for tailnetPort, handler := range serveConfig.TCP {
		if handler.TCPForward == "" {
			continue
		}
		if localPort, ok := proxyPort(handler.TCPForward); ok {
			serves = append(serves, Serve{LocalPort: localPort, TailnetPort: tailnetPort, URL: fmt.Sprintf("tcp://:%d", tailnetPort)})
		}
	}
	return serves
}

// proxyPort reads the local port from a proxy target in any form Tailscale
// accepts: "3000", "localhost:3000" or "http://localhost:3000/path".
func proxyPort(target string) (uint16, bool) {
	if target == "" {
		return 0, false
	}
	if !strings.Contains(target, "://") {
		if _, err := strconv.ParseUint(target, 10, 16); err == nil {
			target = "localhost:" + target
		}
		target = "tcp://" + target
	}
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Port() == "" {
		return 0, false
	}
	port, err := strconv.ParseUint(targetURL.Port(), 10, 16)
	if err != nil || port == 0 {
		return 0, false
	}
	return uint16(port), true
}
