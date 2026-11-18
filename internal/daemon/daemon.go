// Package daemon runs tailserve: it keeps the Served Services in step with the
// running ones and the Allowlist, and serves the Dashboard and the control socket.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/marcomontalbano/tailserve/internal/config"
	"github.com/marcomontalbano/tailserve/internal/control"
	"github.com/marcomontalbano/tailserve/internal/dashboard"
	"github.com/marcomontalbano/tailserve/internal/discovery"
	"github.com/marcomontalbano/tailserve/internal/status"
	"github.com/marcomontalbano/tailserve/internal/tailnet"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// PollInterval is how often tailserve looks for Services and config changes.
const PollInterval = 2 * time.Second

// Run serves the allowed Services until ctx is done or `tailserve stop` asks it to stop.
// version is reported to `tailserve version --daemon`.
// It returns control.ErrAlreadyRunning when another tailserve owns the control socket.
func Run(ctx context.Context, configPath, version string, logger *log.Logger) error {
	controlListener, err := control.Listen()
	if err != nil {
		return err
	}
	defer os.Remove(control.SocketPath())

	if created, err := config.EnsureFile(configPath); err != nil {
		controlListener.Close()
		return err
	} else if created {
		logger.Printf("created %s: nothing is allowed yet", configPath)
	}

	// An invalid config falls back to the default port here; the first poll reports the error.
	startConfig, err := config.Load(configPath)
	if err != nil {
		startConfig.DashboardLocalPort = config.DefaultDashboardLocalPort
	}
	// Tailscale forwards tailnet visitors to tailnetDashboardListener, so its page links to tailnet
	// addresses; the page on the local listener links to localhost.
	tailnetDashboardListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		controlListener.Close()
		return fmt.Errorf("starting the Dashboard: %w", err)
	}
	var localDashboardListener net.Listener
	var dashboardLocalError string
	if startConfig.DashboardLocalPort != 0 {
		localDashboardListener, dashboardLocalError, err = listenDashboardLocally(startConfig.DashboardLocalPort)
		if err != nil {
			controlListener.Close()
			tailnetDashboardListener.Close()
			return fmt.Errorf("starting the Dashboard on this machine: %w", err)
		}
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	d := &daemon{
		configPath:          configPath,
		version:             version,
		allowlist:           allowlist.New(nil, nil),
		scanner:             &discovery.LsofScanner{},
		tailscale:           &local.Client{},
		logger:              logger,
		dashboardTarget:     "http://" + tailnetDashboardListener.Addr().String(),
		dashboardLocalPort:  startConfig.DashboardLocalPort,
		dashboardLocalError: dashboardLocalError,
	}

	controlServer := &http.Server{Handler: control.Handler(d.currentSnapshot, stop)}
	tailnetDashboardServer := &http.Server{Handler: dashboard.Handler(d.currentSnapshot, false)}
	localDashboardServer := &http.Server{Handler: dashboard.Handler(d.currentSnapshot, true)}
	go controlServer.Serve(controlListener)
	go tailnetDashboardServer.Serve(tailnetDashboardListener)
	if localDashboardListener != nil {
		d.dashboardLocalURL = fmt.Sprintf("http://localhost:%d/", localDashboardListener.Addr().(*net.TCPAddr).Port)
		go localDashboardServer.Serve(localDashboardListener)
	}

	d.poll(ctx)
	if snapshot := d.currentSnapshot(); snapshot.DashboardURL != "" {
		logger.Printf("Dashboard: %s", snapshot.DashboardURL)
	}
	if d.dashboardLocalURL != "" {
		logger.Printf("Dashboard on this machine: %s", d.dashboardLocalURL)
	}
	if d.dashboardLocalError != "" {
		logger.Print(d.dashboardLocalError)
	}
	logger.Print("tailserve is running: press Ctrl-C or run `tailserve stop` to stop")

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.closeSession()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = controlServer.Shutdown(shutdownCtx)
			_ = tailnetDashboardServer.Shutdown(shutdownCtx)
			_ = localDashboardServer.Shutdown(shutdownCtx)
			logger.Print("stopped: nothing is served any more")
			return nil
		case <-ticker.C:
			d.poll(ctx)
		}
	}
}

type daemon struct {
	configPath string
	version    string
	// allowlist is the last valid config; an invalid file leaves it unchanged.
	allowlist       allowlist.Allowlist
	configModTime   time.Time
	configError     string
	scanner         discovery.Scanner
	tailscale       *local.Client
	session         *tailnet.Session
	logger          *log.Logger
	dashboardTarget string
	// dashboardLocalPort is the [dashboard] local_port the Dashboard started with; a new one applies at the next start.
	dashboardLocalPort uint16
	// dashboardLocalURL is the Dashboard's address on this machine; empty when the config turns it off.
	dashboardLocalURL string
	// dashboardLocalError says why the Dashboard is not at dashboardLocalPort.
	dashboardLocalError string

	mu       sync.Mutex
	snapshot status.Snapshot
}

func (d *daemon) currentSnapshot() status.Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshot
}

// poll runs one round: find the Services, decide which to serve, and apply that to Tailscale.
func (d *daemon) poll(ctx context.Context) {
	services, err := d.scanner.Scan(ctx)
	if err != nil {
		d.logger.Printf("looking for Services: %v", err)
		return
	}
	// tailserve's own Dashboard listener is not a Service to serve.
	ownPID := os.Getpid()
	services = slices.DeleteFunc(services, func(service discovery.Service) bool { return service.PID == ownPID })
	d.reloadConfigIfChanged()

	snapshot := status.Snapshot{
		Running:     true,
		PID:         os.Getpid(),
		Version:     d.version,
		ConfigPath:  d.configPath,
		RuleCount:   len(d.allowlist.Rules),
		ConfigError: d.configError,

		DashboardLocalURL:   d.dashboardLocalURL,
		DashboardLocalError: d.dashboardLocalError,
	}

	hostname, tailscaleErr := tailnet.Hostname(ctx, d.tailscale)
	snapshot.Hostname = hostname
	if tailscaleErr == nil {
		tailscaleErr = d.ensureSession(ctx)
	}

	var serveConfig *ipn.ServeConfig
	var outsideServes []tailnet.Serve
	var usedTailnetPorts map[uint16]bool
	if tailscaleErr == nil {
		serveConfig, err = d.tailscale.GetServeConfig(ctx)
		if err != nil {
			tailscaleErr = fmt.Errorf("reading the Tailscale Serve config: %w", err)
		} else {
			outsideServes = tailnet.ServesOutsideSession(serveConfig, d.session.ID)
			usedTailnetPorts = tailnet.TailnetPortsOutsideSession(serveConfig, d.session.ID)
		}
	}

	rows := status.Describe(services, d.allowlist, outsideServes, usedTailnetPorts, hostname)

	if tailscaleErr == nil {
		mappings := tailnet.Mappings{Ports: status.ServedPorts(rows), DashboardPort: allowlist.DashboardPort}
		if usedTailnetPorts[allowlist.DashboardPort] {
			snapshot.DashboardError = fmt.Sprintf("tailnet port %d is already used by another mapping", allowlist.DashboardPort)
		} else {
			mappings.DashboardTarget = d.dashboardTarget
			// Without the redirect the Dashboard still works, at its https:// address.
			if !usedTailnetPorts[allowlist.DashboardRedirectPort] {
				mappings.DashboardRedirectPort = allowlist.DashboardRedirectPort
			}
		}
		if err := d.session.Apply(ctx, d.tailscale, serveConfig, hostname, mappings); err != nil {
			tailscaleErr = err
		} else if mappings.DashboardTarget != "" {
			snapshot.DashboardURL = tailnet.DashboardURL(hostname, allowlist.DashboardPort) + "/"
		}
	}
	if tailscaleErr != nil {
		snapshot.TailscaleError = tailscaleErr.Error()
		snapshot.DashboardError = "Tailscale is not available"
		status.MarkNotServed(rows, tailscaleErr.Error())
	}
	snapshot.Rows = rows

	d.mu.Lock()
	previous := d.snapshot
	d.snapshot = snapshot
	d.mu.Unlock()
	d.logChanges(previous, snapshot)
}

// ensureSession opens a Tailscale session when there is none, or when Tailscale ended the previous one.
func (d *daemon) ensureSession(ctx context.Context) error {
	if d.session != nil {
		select {
		case <-d.session.Closed():
			d.logger.Print("the Tailscale session ended: reconnecting")
			d.session = nil
		default:
			return nil
		}
	}
	session, err := tailnet.OpenSession(ctx, d.tailscale)
	if err != nil {
		return err
	}
	d.session = session
	return nil
}

func (d *daemon) closeSession() {
	if d.session != nil {
		_ = d.session.Close()
		<-d.session.Closed()
		d.session = nil
	}
}

// reloadConfigIfChanged reads the config again when the file changed since the
// last read, keeping the previous Allowlist when the new content is invalid.
func (d *daemon) reloadConfigIfChanged() {
	info, err := os.Stat(d.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if !d.configModTime.IsZero() || d.configError == "" {
				d.logger.Printf("%s is missing: nothing is allowed", d.configPath)
			}
			d.allowlist, d.configModTime, d.configError = allowlist.New(nil, nil), time.Time{}, "the config file is missing"
			return
		}
		d.configError = err.Error()
		return
	}
	if info.ModTime().Equal(d.configModTime) {
		return
	}
	d.configModTime = info.ModTime()

	loadedConfig, err := config.Load(d.configPath)
	if err != nil {
		d.configError = err.Error()
		d.logger.Printf("invalid config, keeping the previous one: %v", err)
		return
	}
	d.allowlist, d.configError = loadedConfig.Allowlist, ""
	d.logger.Printf("loaded %s: %d Rules", d.configPath, len(loadedConfig.Allowlist.Rules))
	if loadedConfig.DashboardLocalPort != d.dashboardLocalPort {
		d.logger.Printf("restart tailserve to move the Dashboard on this machine to local_port %d", loadedConfig.DashboardLocalPort)
	}
}

// logChanges prints the Services that became Served or stopped being Served, and Tailscale errors as they appear.
func (d *daemon) logChanges(previous, current status.Snapshot) {
	previousServedPorts := servedPortSet(previous)
	currentServedPorts := servedPortSet(current)
	for _, row := range current.Rows {
		if row.State == status.Served && !previousServedPorts[row.Service.Port] {
			d.logger.Printf("serving %s on %d at %s", row.Service.ProcessName, row.Service.Port, row.Detail)
			previousServedPorts[row.Service.Port] = true
		}
	}
	for _, row := range previous.Rows {
		if row.State == status.Served && !currentServedPorts[row.Service.Port] {
			d.logger.Printf("stopped serving %s on %d", row.Service.ProcessName, row.Service.Port)
			currentServedPorts[row.Service.Port] = true
		}
	}
	if current.TailscaleError != "" && current.TailscaleError != previous.TailscaleError {
		d.logger.Printf("nothing can be served: %s", current.TailscaleError)
	}
}

// servedPortSet returns the ports of the Served rows of snapshot.
func servedPortSet(snapshot status.Snapshot) map[uint16]bool {
	ports := map[uint16]bool{}
	for _, port := range status.ServedPorts(snapshot.Rows) {
		ports[port] = true
	}
	return ports
}

// listenDashboardLocally listens for the Dashboard on 127.0.0.1 at localPort. A taken
// localPort falls back to a random port, and localError says so.
func listenDashboardLocally(localPort uint16) (listener net.Listener, localError string, err error) {
	listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err == nil {
		return listener, "", nil
	}
	localError = fmt.Sprintf("local_port %d is not available, so the Dashboard on this machine uses a random port: %v", localPort, err)
	listener, err = net.Listen("tcp", "127.0.0.1:0")
	return listener, localError, err
}
