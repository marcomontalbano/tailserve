package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
)

func TestAddAndRemoveRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tailserve", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[locked]\nports = [4000]\n\n[dashboard]\nlocal_port = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	vite := allowlist.Rule{Port: &allowlist.PortRange{First: 5173, Last: 5199}, Note: "vite"}
	node := allowlist.Rule{Process: "node"}
	for _, rule := range []allowlist.Rule{vite, node} {
		if added, err := AddRule(path, rule); err != nil || !added {
			t.Fatalf("AddRule(%v) = %v, %v; want added", rule, added, err)
		}
	}
	if added, err := AddRule(path, allowlist.Rule{Process: "NODE"}); err != nil || added {
		t.Errorf("AddRule() of a duplicate = %v, %v; want not added", added, err)
	}
	if _, err := AddRule(path, allowlist.Rule{Port: &allowlist.PortRange{First: 443, Last: 443}}); err == nil {
		t.Error("AddRule() accepted the Dashboard port")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{header, "port = '5173-5199'", "process = 'node'", "ports = [4000]", "local_port = 0"} {
		if !strings.Contains(string(content), expected) {
			t.Errorf("config file is missing %q:\n%s", expected, content)
		}
	}

	removed, err := RemoveRules(path, func(rule allowlist.Rule) bool { return SameRule(rule, vite) })
	if err != nil || len(removed) != 1 || removed[0].Note != "vite" {
		t.Fatalf("RemoveRules() = %v, %v; want the vite Rule", removed, err)
	}

	loadedConfig, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	list := loadedConfig.Allowlist
	if len(list.Rules) != 1 || list.Rules[0].Process != "node" || !list.LockedPorts[4000] || loadedConfig.DashboardLocalPort != 0 {
		t.Errorf("after removing, the config holds %+v", loadedConfig)
	}
}

func TestAddRuleUpdatesNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	rule := allowlist.Rule{Port: &allowlist.PortRange{First: 3000, Last: 3000}}
	if _, err := AddRule(path, rule); err != nil {
		t.Fatal(err)
	}
	rule.Note = "api"
	if _, err := AddRule(path, rule); err != nil {
		t.Fatal(err)
	}
	loadedConfig, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if rules := loadedConfig.Allowlist.Rules; len(rules) != 1 || rules[0].Note != "api" {
		t.Errorf("Rules = %+v, want one Rule with note api", rules)
	}
}
