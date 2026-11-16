package discovery

import (
	"context"
	"net"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestParseLsof(t *testing.T) {
	out := []byte(`p1143
cControlCenter
f9
n*:7000
f10
n*:7000
p4242
cnode
f21
n127.0.0.1:5173
f22
n[::1]:5173
f23
n127.0.0.1:9229
`)

	services, err := parseLsof(out)
	if err != nil {
		t.Fatal(err)
	}

	want := []Service{
		{Port: 5173, ProcessName: "node", PID: 4242, Addresses: []string{"127.0.0.1", "[::1]"}},
		{Port: 7000, ProcessName: "ControlCenter", PID: 1143, Addresses: []string{"*"}},
		{Port: 9229, ProcessName: "node", PID: 4242, Addresses: []string{"127.0.0.1"}},
	}
	if !reflect.DeepEqual(services, want) {
		t.Errorf("parseLsof() =\n%+v\nwant\n%+v", services, want)
	}
}

func TestParseLsofFolders(t *testing.T) {
	out := []byte("p1143\nfcwd\nn/\np4242\nfcwd\nn/Users/me/Projects/website\n")

	foldersByPID, err := parseLsofFolders(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{4242: "/Users/me/Projects/website"}
	if !reflect.DeepEqual(foldersByPID, want) {
		t.Errorf("parseLsofFolders() = %v, want %v", foldersByPID, want)
	}
}

func TestShortFolderAndFolderName(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	tests := []struct {
		folder, wantShort, wantName string
	}{
		{"/Users/me/Projects/website", "~/Projects/website", "website"},
		{"/Users/me", "~", "me"},
		{"/Users/meadow/app", "/Users/meadow/app", "app"},
		{"", "", ""},
	}
	for _, test := range tests {
		service := Service{Folder: test.folder}
		if got := service.ShortFolder(); got != test.wantShort {
			t.Errorf("ShortFolder(%q) = %q, want %q", test.folder, got, test.wantShort)
		}
		if got := service.FolderName(); got != test.wantName {
			t.Errorf("FolderName(%q) = %q, want %q", test.folder, got, test.wantName)
		}
	}
}

func TestParseLsofEmpty(t *testing.T) {
	services, err := parseLsof(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 0 {
		t.Errorf("parseLsof(nil) = %+v, want no Services", services)
	}
}

func TestParseLsofRejectsBadPort(t *testing.T) {
	if _, err := parseLsof([]byte("p1\ncnode\nn*:http\n")); err == nil {
		t.Error("parseLsof() accepted a non-numeric port")
	}
}

func TestLsofScannerFindsOwnListenerAndFolder(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	folder, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &LsofScanner{}
	for scan := 1; scan <= 2; scan++ {
		services, err := scanner.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(services, func(service Service) bool { return service.PID == os.Getpid() && service.Port == port })
		if index < 0 {
			t.Fatalf("scan %d: the test's own listener on %d is missing", scan, port)
		}
		// The second scan reads the folder from the cache.
		if services[index].Folder != folder {
			t.Errorf("scan %d: Folder = %q, want %q", scan, services[index].Folder, folder)
		}
	}
}
