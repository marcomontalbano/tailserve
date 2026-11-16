package allowlist

import "testing"

func portRange(t *testing.T, text string) *PortRange {
	t.Helper()
	r, err := ParsePortRange(text)
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestParsePortRange(t *testing.T) {
	valid := map[string]PortRange{
		"5173":      {5173, 5173},
		"5173-5199": {5173, 5199},
		" 1 - 2 ":   {1, 2},
	}
	for text, want := range valid {
		got, err := ParsePortRange(text)
		if err != nil || got != want {
			t.Errorf("ParsePortRange(%q) = %v, %v; want %v", text, got, err, want)
		}
	}

	for _, text := range []string{"", "0", "65536", "http", "5199-5173", "1-2-3"} {
		if _, err := ParsePortRange(text); err == nil {
			t.Errorf("ParsePortRange(%q) accepted an invalid range", text)
		}
	}
}

func TestDecide(t *testing.T) {
	list := New([]Rule{
		{Port: portRange(t, "5173-5199")},
		{Process: "node"},
		{Process: "python3", Port: portRange(t, "8000")},
		{Port: portRange(t, "5432")},
	}, []uint16{4000})

	tests := []struct {
		name        string
		port        uint16
		processName string
		want        Verdict
	}{
		{"port inside an allowed range", 5180, "bun", Allowed},
		{"any port of an allowed process", 3000, "node", Allowed},
		{"process and port must both match", 8000, "python3", Allowed},
		{"process name ignores case", 8000, "PYTHON3", Allowed},
		{"process matches but port does not", 8001, "python3", NotAllowed},
		{"nothing matches", 3000, "ruby", NotAllowed},
		{"process rule does not unlock a locked port", 9229, "node", Locked},
		{"port added to the locked ports", 4000, "node", Locked},
		{"exact port rule unlocks a locked port", 5432, "postgres", Allowed},
		{"dashboard port is reserved even when a rule matches", 443, "node", Reserved},
		{"dashboard redirect port is reserved too", 80, "node", Reserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := list.Decide(test.port, test.processName)
			if decision.Verdict != test.want {
				t.Errorf("Decide(%d, %q) = %+v, want verdict %d", test.port, test.processName, decision, test.want)
			}
		})
	}
}

func TestRangeDoesNotUnlockLockedPort(t *testing.T) {
	list := New([]Rule{{Port: portRange(t, "9000-9999")}}, nil)
	if decision := list.Decide(9229, "node"); decision.Verdict != Locked {
		t.Errorf("Decide(9229) = %+v, want Locked", decision)
	}
}
