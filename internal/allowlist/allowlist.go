// Package allowlist decides which Services tailserve may serve, and why.
package allowlist

import (
	"fmt"
	"strconv"
	"strings"
)

// DashboardPort is the tailnet port of the Dashboard, served over HTTPS, so no Service is ever served on it.
const DashboardPort uint16 = 443

// DashboardRedirectPort is the tailnet port that redirects plain HTTP to the Dashboard, so no Service is ever served on it either.
const DashboardRedirectPort uint16 = 80

// ReservedPortReason says why port is reserved for the Dashboard, or returns "" when it is not.
func ReservedPortReason(port uint16) string {
	switch port {
	case DashboardPort:
		return fmt.Sprintf("port %d is reserved for the Dashboard", DashboardPort)
	case DashboardRedirectPort:
		return fmt.Sprintf("port %d is reserved to redirect to the Dashboard", DashboardRedirectPort)
	}
	return ""
}

// DefaultLockedPorts are locked on every machine: remote access, file sharing,
// debuggers and databases, which would hand out control or data if served.
var DefaultLockedPorts = []uint16{22, 139, 445, 548, 2345, 3306, 3389, 5432, 5900, 6379, 9229, 27017}

// PortRange is an inclusive range of TCP ports; a single port has First == Last.
type PortRange struct {
	First, Last uint16
}

// ParsePortRange reads "5173" or "5173-5199".
func ParsePortRange(text string) (PortRange, error) {
	firstText, lastText, isRange := strings.Cut(text, "-")
	if !isRange {
		lastText = firstText
	}
	first, err := parsePort(firstText)
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid port range %q: %w", text, err)
	}
	last, err := parsePort(lastText)
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid port range %q: %w", text, err)
	}
	if first > last {
		return PortRange{}, fmt.Errorf("invalid port range %q: %d is greater than %d", text, first, last)
	}
	return PortRange{First: first, Last: last}, nil
}

func parsePort(text string) (uint16, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(text), 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("%q is not a port between 1 and 65535", text)
	}
	return uint16(port), nil
}

// Contains reports whether port is inside the range.
func (r PortRange) Contains(port uint16) bool {
	return r.First <= port && port <= r.Last
}

// IsSinglePort reports whether the range names exactly one port.
func (r PortRange) IsSinglePort() bool {
	return r.First == r.Last
}

func (r PortRange) String() string {
	if r.IsSinglePort() {
		return strconv.Itoa(int(r.First))
	}
	return fmt.Sprintf("%d-%d", r.First, r.Last)
}

// Rule is one entry of the Allowlist. A nil Port matches any port and an empty
// Process matches any Process name, ignoring case; at least one of the two is set.
type Rule struct {
	Port    *PortRange
	Process string
	Note    string
}

// Matches reports whether the Service on port, run by processName, satisfies the Rule.
func (r Rule) Matches(port uint16, processName string) bool {
	if r.Port != nil && !r.Port.Contains(port) {
		return false
	}
	// Ignoring case lets `python` match the `Python` that macOS reports.
	return r.Process == "" || strings.EqualFold(r.Process, processName)
}

// NamesExactPort reports whether the Rule names port on its own, which is the only way to unlock a Locked port.
func (r Rule) NamesExactPort(port uint16) bool {
	return r.Port != nil && r.Port.IsSinglePort() && r.Port.First == port
}

func (r Rule) String() string {
	var parts []string
	if r.Port != nil {
		parts = append(parts, "port "+r.Port.String())
	}
	if r.Process != "" {
		parts = append(parts, "process "+r.Process)
	}
	return strings.Join(parts, ", ")
}

// Allowlist is the set of Rules plus the Locked ports they have to respect.
type Allowlist struct {
	Rules []Rule
	// LockedPorts holds DefaultLockedPorts and the ports the config adds to them.
	LockedPorts map[uint16]bool
}

// New builds an Allowlist whose Locked ports are the defaults plus extraLockedPorts.
func New(rules []Rule, extraLockedPorts []uint16) Allowlist {
	lockedPorts := map[uint16]bool{}
	for _, port := range DefaultLockedPorts {
		lockedPorts[port] = true
	}
	for _, port := range extraLockedPorts {
		lockedPorts[port] = true
	}
	return Allowlist{Rules: rules, LockedPorts: lockedPorts}
}

// Verdict is the outcome of checking a Service against the Allowlist.
type Verdict int

const (
	// Allowed means a Rule matches and nothing stops the Service from being served.
	Allowed Verdict = iota
	// NotAllowed means no Rule matches the Service.
	NotAllowed
	// Locked means a Rule matches, but the port is Locked and no Rule names it exactly.
	Locked
	// Reserved means the Dashboard uses the port.
	Reserved
)

// Decision is a Verdict with the reason a person reads in `tailserve status`.
type Decision struct {
	Verdict Verdict
	Reason  string
}

// Decide checks the Service on port, run by processName, against the Allowlist.
func (a Allowlist) Decide(port uint16, processName string) Decision {
	if reason := ReservedPortReason(port); reason != "" {
		return Decision{Verdict: Reserved, Reason: reason}
	}

	var matchingRules []Rule
	for _, rule := range a.Rules {
		if rule.Matches(port, processName) {
			matchingRules = append(matchingRules, rule)
		}
	}
	if len(matchingRules) == 0 {
		return Decision{Verdict: NotAllowed, Reason: "no Rule matches"}
	}

	if a.LockedPorts[port] {
		for _, rule := range matchingRules {
			if rule.NamesExactPort(port) {
				return Decision{Verdict: Allowed, Reason: "allowed by Rule " + rule.String()}
			}
		}
		return Decision{Verdict: Locked, Reason: fmt.Sprintf("port %d is locked: only a Rule naming exactly this port serves it", port)}
	}
	return Decision{Verdict: Allowed, Reason: "allowed by Rule " + matchingRules[0].String()}
}
