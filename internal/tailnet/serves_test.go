package tailnet

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"tailscale.com/ipn"
)

func TestProxyPort(t *testing.T) {
	valid := map[string]uint16{
		"3000":                            3000,
		"localhost:3000":                  3000,
		"http://localhost:5180":           5180,
		"http://127.0.0.1:5180/foo":       5180,
		"https+insecure://localhost:8443": 8443,
	}
	for target, want := range valid {
		if got, ok := proxyPort(target); !ok || got != want {
			t.Errorf("proxyPort(%q) = %d, %v; want %d", target, got, ok, want)
		}
	}
	for _, target := range []string{"", "unix:/tmp/app.sock", "http://localhost"} {
		if got, ok := proxyPort(target); ok {
			t.Errorf("proxyPort(%q) = %d, want no port", target, got)
		}
	}
}

func TestServesOf(t *testing.T) {
	serveConfig := &ipn.ServeConfig{
		TCP: map[uint16]*ipn.TCPPortHandler{
			5180: {HTTPS: true},
			8080: {HTTP: true},
			5432: {TCPForward: "127.0.0.1:5432"},
		},
		Web: map[ipn.HostPort]*ipn.WebServerConfig{
			"machine.tailnet.ts.net:5180": {Handlers: map[string]*ipn.HTTPHandler{"/": {Proxy: "http://localhost:5180"}}},
			"machine.tailnet.ts.net:8080": {Handlers: map[string]*ipn.HTTPHandler{"/api/": {Proxy: "3001"}}},
		},
	}

	var got []string
	for _, serve := range servesOf(serveConfig) {
		got = append(got, fmt.Sprintf("%s -> %d", serve.URL, serve.LocalPort))
	}
	slices.Sort(got)
	want := []string{
		"http://machine.tailnet.ts.net:8080/api -> 3001",
		"https://machine.tailnet.ts.net:5180 -> 5180",
		"tcp://:5432 -> 5432",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("servesOf() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOutsideSession(t *testing.T) {
	serveConfig := &ipn.ServeConfig{
		TCP: map[uint16]*ipn.TCPPortHandler{443: {HTTPS: true}},
		Web: map[ipn.HostPort]*ipn.WebServerConfig{
			"machine.tailnet.ts.net:443": {Handlers: map[string]*ipn.HTTPHandler{"/": {Path: "/var/www"}}},
		},
		Foreground: map[string]*ipn.ServeConfig{
			"own": {
				TCP: map[uint16]*ipn.TCPPortHandler{5173: {HTTPS: true}},
				Web: map[ipn.HostPort]*ipn.WebServerConfig{
					"machine.tailnet.ts.net:5173": {Handlers: map[string]*ipn.HTTPHandler{"/": {Proxy: "http://localhost:5173"}}},
				},
			},
			"other": {
				TCP: map[uint16]*ipn.TCPPortHandler{5180: {HTTPS: true}},
				Web: map[ipn.HostPort]*ipn.WebServerConfig{
					"machine.tailnet.ts.net:5180": {Handlers: map[string]*ipn.HTTPHandler{"/": {Proxy: "http://localhost:5180"}}},
				},
			},
		},
	}

	serves := ServesOutsideSession(serveConfig, "own")
	if len(serves) != 1 || serves[0].LocalPort != 5180 {
		t.Errorf("ServesOutsideSession() = %+v, want only the mapping to 5180", serves)
	}

	usedPorts := TailnetPortsOutsideSession(serveConfig, "own")
	if !usedPorts[443] || !usedPorts[5180] || usedPorts[5173] {
		t.Errorf("TailnetPortsOutsideSession() = %v, want 443 and 5180 but not the own 5173", usedPorts)
	}
}
