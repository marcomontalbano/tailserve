package config

import (
	"path/filepath"
	"testing"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
)

func TestParse(t *testing.T) {
	loadedConfig, err := Parse([]byte(`
[[allow]]
port = "5173-5199"
note = "vite"

[[allow]]
process = "node"

[[allow]]
process = "python3"
port = 8000

[locked]
ports = [4000]

[dashboard]
local_port = 8765
`))
	if err != nil {
		t.Fatal(err)
	}
	list := loadedConfig.Allowlist
	if loadedConfig.DashboardLocalPort != 8765 {
		t.Errorf("DashboardLocalPort = %d, want 8765", loadedConfig.DashboardLocalPort)
	}

	if len(list.Rules) != 3 {
		t.Fatalf("got %d Rules, want 3", len(list.Rules))
	}
	if got := list.Rules[0]; got.Port.String() != "5173-5199" || got.Note != "vite" {
		t.Errorf("first Rule = %+v", got)
	}
	if got := list.Rules[2]; got.Port.String() != "8000" || got.Process != "python3" {
		t.Errorf("third Rule = %+v", got)
	}
	if !list.LockedPorts[4000] || !list.LockedPorts[9229] || !list.LockedPorts[2345] {
		t.Errorf("LockedPorts = %v, want the defaults plus 4000", list.LockedPorts)
	}
}

func TestParseRejectsInvalidConfig(t *testing.T) {
	invalid := map[string]string{
		"rule without port or process": "[[allow]]\nnote = \"nothing\"\n",
		"unknown key":                  "[[allow]]\nprocces = \"node\"\n",
		"port out of range":            "[[allow]]\nport = 70000\n",
		"bad range":                    "[[allow]]\nport = \"5199-5173\"\n",
		"port of wrong type":           "[[allow]]\nport = true\n",
		"dashboard port":               "[[allow]]\nport = 443\n",
		"dashboard redirect port":      "[[allow]]\nport = 80\n",
		"locked port out of range":     "[locked]\nports = [0]\n",
		"local port needing root":      "[dashboard]\nlocal_port = 80\n",
		"local port out of range":      "[dashboard]\nlocal_port = 70000\n",
	}
	for name, content := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(content)); err == nil {
				t.Errorf("Parse() accepted:\n%s", content)
			}
		})
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	loadedConfig, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if decision := loadedConfig.Allowlist.Decide(5173, "node"); decision.Verdict != allowlist.NotAllowed {
		t.Errorf("Decide() on a missing config = %+v, want NotAllowed", decision)
	}
	if loadedConfig.DashboardLocalPort != DefaultDashboardLocalPort {
		t.Errorf("DashboardLocalPort = %d, want the default %d", loadedConfig.DashboardLocalPort, DefaultDashboardLocalPort)
	}
}

func TestParseDashboardLocalPort(t *testing.T) {
	for content, want := range map[string]uint16{
		"":                                 DefaultDashboardLocalPort,
		"[dashboard]\n":                    DefaultDashboardLocalPort,
		"[dashboard]\nlocal_port = 0\n":    0,
		"[dashboard]\nlocal_port = 9999\n": 9999,
	} {
		loadedConfig, err := Parse([]byte(content))
		if err != nil {
			t.Fatalf("Parse(%q): %v", content, err)
		}
		if loadedConfig.DashboardLocalPort != want {
			t.Errorf("Parse(%q).DashboardLocalPort = %d, want %d", content, loadedConfig.DashboardLocalPort, want)
		}
	}
}
