// Package status explains, for each Service, whether it is Served and why.
package status

import (
	"fmt"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/marcomontalbano/tailserve/internal/discovery"
	"github.com/marcomontalbano/tailserve/internal/tailnet"
)

// State is what `tailserve status` shows for one Service.
type State string

const (
	// Served means tailserve made the Service reachable on the tailnet; Detail is its URL.
	Served State = "served"
	// ServedOutsideTailserve means a mapping tailserve did not create already reaches the Service.
	ServedOutsideTailserve State = "served outside tailserve"
	NotAllowed             State = "not allowed"
	Locked                 State = "locked"
	Reserved               State = "reserved"
	// NotServed means the Service is allowed, but something keeps it off the tailnet; Detail says what.
	NotServed State = "not served"
)

// Row is one line of `tailserve status`.
type Row struct {
	Service discovery.Service
	State   State
	// Detail is the tailnet URL of a Served Service, or the reason it is not Served.
	Detail string
}

// Snapshot is everything `tailserve status` and the Dashboard show.
type Snapshot struct {
	Running bool
	PID     int
	// Version is the version of the running tailserve, such as "0.1.0"; empty when it is not running.
	Version string
	// Hostname is the machine's name on the tailnet; empty when Tailscale is not connected.
	Hostname string
	// DashboardURL is empty when the Dashboard is not served; DashboardError then says why, if known.
	DashboardURL   string
	DashboardError string
	// DashboardLocalURL is the Dashboard's address on this machine, such as "http://localhost:7171/"; empty when turned off.
	DashboardLocalURL string
	// DashboardLocalError says why DashboardLocalURL is not at the configured local_port.
	DashboardLocalError string
	ConfigPath          string
	RuleCount           int
	// ConfigError is set when the config file is invalid; a running tailserve keeps using the last valid one.
	ConfigError    string
	TailscaleError string
	Rows           []Row
}

// Describe decides the State of each Service. Allowed Services come out Served
// at the same port of hostname, unless usedTailnetPorts already holds that port;
// callers turn them into NotServed with MarkNotServed when they cannot serve them.
func Describe(services []discovery.Service, list allowlist.Allowlist, outsideServes []tailnet.Serve, usedTailnetPorts map[uint16]bool, hostname string) []Row {
	urlsByLocalPort := map[uint16]string{}
	for _, serve := range outsideServes {
		if _, found := urlsByLocalPort[serve.LocalPort]; !found {
			urlsByLocalPort[serve.LocalPort] = serve.URL
		}
	}

	rows := make([]Row, 0, len(services))
	for _, service := range services {
		if url, isServed := urlsByLocalPort[service.Port]; isServed {
			rows = append(rows, Row{Service: service, State: ServedOutsideTailserve, Detail: url})
			continue
		}

		decision := list.Decide(service.Port, service.ProcessName)
		switch decision.Verdict {
		case allowlist.Allowed:
			if usedTailnetPorts[service.Port] {
				rows = append(rows, Row{Service: service, State: NotServed, Detail: fmt.Sprintf("tailnet port %d is already used by another mapping", service.Port)})
			} else {
				rows = append(rows, Row{Service: service, State: Served, Detail: fmt.Sprintf("https://%s:%d", hostname, service.Port)})
			}
		case allowlist.NotAllowed:
			rows = append(rows, Row{Service: service, State: NotAllowed, Detail: decision.Reason})
		case allowlist.Locked:
			rows = append(rows, Row{Service: service, State: Locked, Detail: decision.Reason})
		case allowlist.Reserved:
			rows = append(rows, Row{Service: service, State: Reserved, Detail: decision.Reason})
		}
	}
	return rows
}

// MarkNotServed turns every Served row into NotServed with reason, for when the mappings could not be applied.
func MarkNotServed(rows []Row, reason string) {
	for index := range rows {
		if rows[index].State == Served {
			rows[index].State = NotServed
			rows[index].Detail = reason
		}
	}
}

// ServedPorts returns the distinct ports of the Served rows.
func ServedPorts(rows []Row) []uint16 {
	var ports []uint16
	seen := map[uint16]bool{}
	for _, row := range rows {
		if row.State == Served && !seen[row.Service.Port] {
			seen[row.Service.Port] = true
			ports = append(ports, row.Service.Port)
		}
	}
	return ports
}
