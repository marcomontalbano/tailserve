package daemon

import (
	"net"
	"strings"
	"testing"
)

func TestListenDashboardLocallyFallsBackWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	takenPort := uint16(taken.Addr().(*net.TCPAddr).Port)

	listener, localError, err := listenDashboardLocally(takenPort)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if port := uint16(listener.Addr().(*net.TCPAddr).Port); port == takenPort {
		t.Errorf("listenDashboardLocally() listened on the taken port %d", port)
	}
	if !strings.Contains(localError, "is not available") {
		t.Errorf("localError = %q, want it to say the port is not available", localError)
	}
}

func TestListenDashboardLocallyOnAFreePort(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := uint16(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()

	listener, localError, err := listenDashboardLocally(freePort)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if port := uint16(listener.Addr().(*net.TCPAddr).Port); port != freePort || localError != "" {
		t.Errorf("listenDashboardLocally(%d) = port %d, %q; want that port and no error", freePort, port, localError)
	}
}
