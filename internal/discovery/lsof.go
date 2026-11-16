package discovery

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// LsofScanner finds Services by running lsof, so it sees only the processes of the current user.
// Keep one LsofScanner across scans: it remembers what it learned about each process instead of asking again.
type LsofScanner struct {
	detailsByProcess map[processKey]processDetails
}

// processDetails is what a process keeps for its whole life: where it runs and what it runs.
type processDetails struct {
	folder     string
	repository *Repository
	tool       string
}

// processKey identifies a process; the name guards against a PID reused by another program.
type processKey struct {
	pid         int
	processName string
}

// Scan returns one Service per process and port, with the folder each process runs in.
func (s *LsofScanner) Scan(ctx context.Context) ([]Service, error) {
	// +c 0 prints the full process name instead of its first 9 characters;
	// -F pcn prints one field per line: p<pid>, c<name>, n<address:port>.
	out, err := runLsof(ctx, "-nP", "-iTCP", "-sTCP:LISTEN", "+c", "0", "-F", "pcn")
	if err != nil {
		return nil, err
	}
	services, err := parseLsof(out)
	if err != nil {
		return nil, err
	}

	if err := s.updateDetails(ctx, services); err != nil {
		return nil, err
	}
	for index := range services {
		details := s.detailsByProcess[processKey{services[index].PID, services[index].ProcessName}]
		services[index].Folder = details.folder
		services[index].Repository = details.repository
		services[index].Tool = details.tool
	}
	return services, nil
}

// updateDetails looks up the folder, Repository and Tool of every process in
// services not seen before, and forgets the processes that no longer listen.
func (s *LsofScanner) updateDetails(ctx context.Context, services []Service) error {
	if s.detailsByProcess == nil {
		s.detailsByProcess = map[processKey]processDetails{}
	}
	listeningProcesses := map[processKey]bool{}
	var newPIDs []string
	for _, service := range services {
		key := processKey{service.PID, service.ProcessName}
		if listeningProcesses[key] {
			continue
		}
		listeningProcesses[key] = true
		if _, known := s.detailsByProcess[key]; !known {
			newPIDs = append(newPIDs, strconv.Itoa(service.PID))
		}
	}
	for key := range s.detailsByProcess {
		if !listeningProcesses[key] {
			delete(s.detailsByProcess, key)
		}
	}
	if len(newPIDs) == 0 {
		return nil
	}

	// -a joins the filters: the cwd entry of these processes only.
	out, err := runLsof(ctx, "-a", "-d", "cwd", "-p", strings.Join(newPIDs, ","), "-F", "pn")
	if err != nil {
		return err
	}
	foldersByPID, err := parseLsofFolders(out)
	if err != nil {
		return err
	}
	for key := range listeningProcesses {
		if _, known := s.detailsByProcess[key]; known {
			continue
		}
		// A process gone in the meantime, or run from "/", gets no folder.
		folder := foldersByPID[key.pid]
		details := processDetails{folder: folder, repository: findRepository(folder)}
		if arguments, err := processArguments(key.pid); err == nil {
			details.tool = toolOf(arguments)
		}
		s.detailsByProcess[key] = details
	}
	return nil
}

// runLsof runs lsof with args and returns what it printed.
func runLsof(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "lsof", args...).Output()
	if err != nil {
		// lsof exits 1 both when nothing matches and when it could not inspect some
		// process; either way the output it did print is valid.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("running lsof: %w", err)
		}
	}
	return out, nil
}

// parseLsofFolders reads the output of `lsof -d cwd -F pn` into the folder of each PID, leaving out "/".
func parseLsofFolders(out []byte) (map[int]string, error) {
	foldersByPID := map[int]string{}
	var pid int
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			parsedPID, err := strconv.Atoi(line[1:])
			if err != nil {
				return nil, fmt.Errorf("parsing lsof pid %q: %w", line[1:], err)
			}
			pid = parsedPID
		case 'n':
			if folder := line[1:]; folder != "/" {
				foldersByPID[pid] = folder
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading lsof output: %w", err)
	}
	return foldersByPID, nil
}

// parseLsof reads the output of `lsof -F pcn`, merging the IPv4 and IPv6
// listeners of one process on one port into a single Service.
func parseLsof(out []byte) ([]Service, error) {
	type serviceKey struct {
		pid  int
		port uint16
	}
	servicesByKey := map[serviceKey]*Service{}
	var pid int
	var processName string

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		field, value := line[0], line[1:]
		switch field {
		case 'p':
			parsedPID, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parsing lsof pid %q: %w", value, err)
			}
			pid, processName = parsedPID, ""
		case 'c':
			processName = value
		case 'n':
			separator := strings.LastIndexByte(value, ':')
			if separator < 0 {
				return nil, fmt.Errorf("parsing lsof address %q: missing port", value)
			}
			address := value[:separator]
			port, err := strconv.ParseUint(value[separator+1:], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("parsing lsof address %q: %w", value, err)
			}
			key := serviceKey{pid: pid, port: uint16(port)}
			service, found := servicesByKey[key]
			if !found {
				service = &Service{Port: uint16(port), ProcessName: processName, PID: pid}
				servicesByKey[key] = service
			}
			if !slices.Contains(service.Addresses, address) {
				service.Addresses = append(service.Addresses, address)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading lsof output: %w", err)
	}

	services := make([]Service, 0, len(servicesByKey))
	for _, service := range servicesByKey {
		services = append(services, *service)
	}
	slices.SortFunc(services, func(a, b Service) int {
		return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.PID, b.PID))
	})
	return services, nil
}
