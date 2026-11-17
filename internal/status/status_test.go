package status

import (
	"slices"
	"testing"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/marcomontalbano/tailserve/internal/discovery"
	"github.com/marcomontalbano/tailserve/internal/tailnet"
)

func TestDescribe(t *testing.T) {
	services := []discovery.Service{
		{Port: 80, ProcessName: "node"},
		{Port: 3000, ProcessName: "ruby"},
		{Port: 5173, ProcessName: "node"},
		{Port: 5180, ProcessName: "node"},
		{Port: 8443, ProcessName: "node"},
		{Port: 9229, ProcessName: "node"},
	}
	list := allowlist.New([]allowlist.Rule{{Process: "node"}}, nil)
	outsideServes := []tailnet.Serve{{LocalPort: 5180, TailnetPort: 5180, URL: "https://machine.tailnet.ts.net:5180"}}
	usedTailnetPorts := map[uint16]bool{5180: true, 8443: true}

	rows := Describe(services, list, outsideServes, usedTailnetPorts, "machine.tailnet.ts.net")

	want := []Row{
		{State: Reserved},
		{State: NotAllowed},
		{State: Served, Detail: "https://machine.tailnet.ts.net:5173"},
		{State: ServedOutsideTailserve, Detail: "https://machine.tailnet.ts.net:5180"},
		{State: NotServed, Detail: "tailnet port 8443 is already used by another mapping"},
		{State: Locked},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for index, row := range rows {
		if row.State != want[index].State {
			t.Errorf("port %d: state %q, want %q", row.Service.Port, row.State, want[index].State)
		}
		if want[index].Detail != "" && row.Detail != want[index].Detail {
			t.Errorf("port %d: detail %q, want %q", row.Service.Port, row.Detail, want[index].Detail)
		}
	}
}

func TestMarkNotServedAndServedPorts(t *testing.T) {
	rows := []Row{
		{Service: discovery.Service{Port: 5173}, State: Served},
		{Service: discovery.Service{Port: 5173, PID: 2}, State: Served},
		{Service: discovery.Service{Port: 5180}, State: ServedOutsideTailserve},
	}
	if got := ServedPorts(rows); !slices.Equal(got, []uint16{5173}) {
		t.Errorf("ServedPorts() = %v, want [5173]", got)
	}

	MarkNotServed(rows, "Tailscale is stopped")
	if rows[0].State != NotServed || rows[0].Detail != "Tailscale is stopped" {
		t.Errorf("MarkNotServed() left %+v", rows[0])
	}
	if rows[2].State != ServedOutsideTailserve {
		t.Errorf("MarkNotServed() changed a Service served outside tailserve: %+v", rows[2])
	}
}
