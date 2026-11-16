// Package config reads the tailserve config file: the Allowlist and the Dashboard settings.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/pelletier/go-toml/v2"
)

// Path returns where the config file lives: $XDG_CONFIG_HOME/tailserve/config.toml, or ~/.config/tailserve/config.toml.
func Path() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the home directory: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "tailserve", "config.toml"), nil
}

// header opens every config file tailserve writes, as a reminder of the format.
const header = `# tailserve config. Nothing is served until a Rule below allows it.
#
# [[allow]]
# port = "5173-5199"   # a port or a range of ports
# process = "node"     # a Process name; with a port too, both must match
# note = "vite"        # a reminder of what the Rule is for
#
# [locked]
# ports = [4000]       # more Locked ports: only a Rule naming one exactly serves it
#
# [dashboard]
# local_port = 7171    # the Dashboard at http://localhost:7171; 0 turns it off; applies at the next start
`

// DefaultDashboardLocalPort is the port of the Dashboard on this machine when the config names none.
const DefaultDashboardLocalPort = 7171

// Config is the content of the config file.
type Config struct {
	Allowlist allowlist.Allowlist
	// DashboardLocalPort is the port of the Dashboard on 127.0.0.1; 0 turns that address off.
	DashboardLocalPort uint16
}

// EnsureFile creates an empty config file at path when none exists, and reports whether it did.
func EnsureFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		return false, fmt.Errorf("creating %s: %w", path, err)
	}
	return true, nil
}

// Load reads the config file at path. A missing file is an empty Allowlist, so nothing is served.
func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Allowlist: allowlist.New(nil, nil), DashboardLocalPort: DefaultDashboardLocalPort}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	loadedConfig, err := Parse(content)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return loadedConfig, nil
}

// configFile is the shape of the TOML file.
type configFile struct {
	Allow     []ruleEntry     `toml:"allow,omitempty"`
	Locked    *lockedTable    `toml:"locked,omitempty"`
	Dashboard *dashboardTable `toml:"dashboard,omitempty"`
}

// dashboardTable is the [dashboard] table.
type dashboardTable struct {
	// LocalPort is nil when the file does not set it, so the default applies.
	LocalPort *int64 `toml:"local_port,omitempty"`
}

// lockedTable is the [locked] table.
type lockedTable struct {
	Ports []int64 `toml:"ports"`
}

// ruleEntry is one [[allow]] table.
type ruleEntry struct {
	// Port is an integer (5173) or a string holding a port or a range ("5173-5199").
	Port    any    `toml:"port,omitempty"`
	Process string `toml:"process,omitempty"`
	Note    string `toml:"note,omitempty"`
}

// Parse reads the content of a config file, rejecting unknown keys so a typo does not silently allow nothing.
func Parse(content []byte) (Config, error) {
	file, err := decode(content)
	if err != nil {
		return Config{}, err
	}
	return file.config()
}

// decode reads the TOML content without checking the values.
func decode(content []byte) (configFile, error) {
	var file configFile
	decoder := toml.NewDecoder(bytes.NewReader(content)).DisallowUnknownFields()
	err := decoder.Decode(&file)
	return file, err
}

// config checks every value of the file and turns it into a Config.
func (file configFile) config() (Config, error) {
	list, err := file.allowlist()
	if err != nil {
		return Config{}, err
	}
	loadedConfig := Config{Allowlist: list, DashboardLocalPort: DefaultDashboardLocalPort}
	if file.Dashboard != nil && file.Dashboard.LocalPort != nil {
		port := *file.Dashboard.LocalPort
		switch {
		case port == 0:
		case port < 1024 || port > 65535:
			// Ports below 1024 need root on macOS, which tailserve does not run as.
			return Config{}, fmt.Errorf("[dashboard] local_port: %d is not 0 or a port between 1024 and 65535", port)
		}
		loadedConfig.DashboardLocalPort = uint16(port)
	}
	return loadedConfig, nil
}

// allowlist checks the [[allow]] and [locked] values of the file and turns them into an Allowlist.
func (file configFile) allowlist() (allowlist.Allowlist, error) {
	rules := make([]allowlist.Rule, 0, len(file.Allow))
	for index, entry := range file.Allow {
		rule, err := parseRule(entry)
		if err != nil {
			return allowlist.Allowlist{}, fmt.Errorf("[[allow]] number %d: %w", index+1, err)
		}
		rules = append(rules, rule)
	}

	var lockedPorts []uint16
	var lockedPortEntries []int64
	if file.Locked != nil {
		lockedPortEntries = file.Locked.Ports
	}
	for _, port := range lockedPortEntries {
		if port < 1 || port > 65535 {
			return allowlist.Allowlist{}, fmt.Errorf("[locked] ports: %d is not a port between 1 and 65535", port)
		}
		lockedPorts = append(lockedPorts, uint16(port))
	}
	return allowlist.New(rules, lockedPorts), nil
}

func parseRule(entry ruleEntry) (allowlist.Rule, error) {
	rule := allowlist.Rule{Process: entry.Process, Note: entry.Note}

	switch port := entry.Port.(type) {
	case nil:
	case int64:
		if port < 1 || port > 65535 {
			return rule, fmt.Errorf("port %d is not a port between 1 and 65535", port)
		}
		rule.Port = &allowlist.PortRange{First: uint16(port), Last: uint16(port)}
	case string:
		portRange, err := allowlist.ParsePortRange(port)
		if err != nil {
			return rule, err
		}
		rule.Port = &portRange
	default:
		return rule, fmt.Errorf("port must be a number or a string like \"5173-5199\", not %v", port)
	}

	if rule.Port == nil && rule.Process == "" {
		return rule, errors.New("a Rule needs a port, a process, or both")
	}
	if rule.Port != nil && rule.Port.IsSinglePort() {
		if reason := allowlist.ReservedPortReason(rule.Port.First); reason != "" {
			return rule, errors.New(reason)
		}
	}
	return rule, nil
}
