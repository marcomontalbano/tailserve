package discovery

import "testing"

func TestToolWithProcessName(t *testing.T) {
	for _, test := range []struct {
		service Service
		want    string
	}{
		{Service{ProcessName: "node", Tool: "astro dev"}, "astro dev (node)"},
		{Service{ProcessName: "ControlCenter"}, "ControlCenter"},
		{Service{ProcessName: "bun", Tool: "bun"}, "bun"},
		{Service{ProcessName: "Python", Tool: "python"}, "Python"},
	} {
		if got := test.service.ToolWithProcessName(); got != test.want {
			t.Errorf("ToolWithProcessName(%+v) = %q, want %q", test.service, got, test.want)
		}
	}
}
