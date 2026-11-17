// Command tailserve serves the allowed development servers of this machine on the tailnet.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/marcomontalbano/tailserve/internal/config"
	"github.com/marcomontalbano/tailserve/internal/control"
	"github.com/marcomontalbano/tailserve/internal/daemon"
	"github.com/marcomontalbano/tailserve/internal/discovery"
	"github.com/marcomontalbano/tailserve/internal/status"
	"github.com/marcomontalbano/tailserve/internal/tailnet"
	"github.com/marcomontalbano/tailserve/internal/wizard"
	"golang.org/x/term"
	"tailscale.com/client/local"
)

const usage = `tailserve serves the allowed development servers of this machine on your tailnet.

Usage:
  tailserve start     Serve the allowed Services until stopped, with the Dashboard at https://<machine>/ and http://localhost:7171/
  tailserve stop      Stop the running tailserve; everything it served goes offline
  tailserve status    Show the Services that are allowed or served, and why
      --all           Also show the Services no Rule matches
  tailserve allow     Pick running Services to serve, or add a Rule directly:
      --port          A port or a range, such as 5173 or 5173-5199
      --process       A Process name, such as node; with --port, both must match
      --note          A reminder of what the Rule is for
  tailserve deny      Pick Rules to remove, or remove the Rule given by --port and --process
  tailserve config    Open the config file in $EDITOR
  tailserve help      Show this help
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}

	var err error
	switch command := os.Args[1]; command {
	case "start":
		err = start()
	case "stop":
		err = stop()
	case "status":
		flags := flag.NewFlagSet("status", flag.ExitOnError)
		showAll := flags.Bool("all", false, "also show the Services no Rule matches")
		_ = flags.Parse(os.Args[2:])
		err = printStatus(*showAll)
	case "allow":
		flags := flag.NewFlagSet("allow", flag.ExitOnError)
		port := flags.String("port", "", "a port or a range, such as 5173 or 5173-5199")
		process := flags.String("process", "", "a Process name, such as node")
		note := flags.String("note", "", "a reminder of what the Rule is for")
		_ = flags.Parse(os.Args[2:])
		err = allow(*port, *process, *note)
	case "deny":
		flags := flag.NewFlagSet("deny", flag.ExitOnError)
		port := flags.String("port", "", "the port or range of the Rule to remove")
		process := flags.String("process", "", "the Process name of the Rule to remove")
		_ = flags.Parse(os.Args[2:])
		err = deny(*port, *process)
	case "config":
		err = editConfig()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "tailserve: unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}
	if errors.Is(err, wizard.ErrCancelled) {
		fmt.Println("Cancelled: nothing changed.")
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tailserve:", err)
		os.Exit(1)
	}
}

// start runs tailserve in the foreground until Ctrl-C, a termination signal, or `tailserve stop`.
func start() error {
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err = daemon.Run(ctx, configPath, log.New(os.Stdout, "", log.Ltime))
	if errors.Is(err, control.ErrAlreadyRunning) {
		return errors.New("tailserve is already running: run `tailserve status` to see what it serves")
	}
	return err
}

// stop asks the running tailserve to stop and waits until it has.
func stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := control.RequestStop(ctx); err != nil {
		return err
	}
	fmt.Println("tailserve stopped: nothing is served any more")
	return nil
}

// printStatus prints the Snapshot of the running tailserve or, when none runs,
// works out what it would serve. Rows no Rule matches are left out unless showAll is set.
func printStatus(showAll bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := currentSnapshot(ctx)
	if err != nil {
		return err
	}
	return printSnapshot(snapshot, showAll)
}

// currentSnapshot returns the Snapshot of the running tailserve or, when none runs, works out what it would serve.
func currentSnapshot(ctx context.Context) (status.Snapshot, error) {
	snapshot, err := control.FetchSnapshot(ctx)
	if errors.Is(err, control.ErrNotRunning) {
		return describeWithoutTailserve(ctx)
	}
	return snapshot, err
}

// allow adds the Rule given by port, process and note or, when neither port nor
// process is given, asks which running Services to serve.
func allow(portText, process, note string) error {
	configPath, err := config.Path()
	if err != nil {
		return err
	}

	var rules []allowlist.Rule
	if portText == "" && process == "" {
		if note != "" {
			return errors.New("--note needs --port or --process")
		}
		if rules, err = chooseRulesToAdd(); err != nil {
			return err
		}
		if len(rules) == 0 {
			fmt.Println("Nothing chosen: nothing changed.")
			return nil
		}
	} else {
		rule, err := ruleFromFlags(portText, process)
		if err != nil {
			return err
		}
		rule.Note = note
		rules = []allowlist.Rule{rule}
	}

	hasAddedRule := false
	for _, rule := range rules {
		added, err := config.AddRule(configPath, rule)
		if err != nil {
			return err
		}
		if added {
			hasAddedRule = true
			fmt.Printf("Allowed %s.\n", rule)
		} else {
			fmt.Printf("Already allowed: %s.\n", rule)
		}
	}
	if hasAddedRule {
		printWhenItApplies()
	}
	return nil
}

// chooseRulesToAdd runs the allow wizard over the running Services that are not served yet.
func chooseRulesToAdd() ([]allowlist.Rule, error) {
	if err := requireTerminal("allow"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := currentSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	var unservedRows []status.Row
	for _, row := range snapshot.Rows {
		if row.State == status.NotAllowed || row.State == status.Locked {
			unservedRows = append(unservedRows, row)
		}
	}
	if len(unservedRows) == 0 {
		fmt.Println("Every running Service is already allowed.")
		return nil, nil
	}
	return wizard.ChooseRulesToAdd(unservedRows)
}

// deny removes the Rule given by port and process or, when neither is given, asks which Rules to remove.
func deny(portText, process string) error {
	configPath, err := config.Path()
	if err != nil {
		return err
	}

	var isChosen func(allowlist.Rule) bool
	if portText == "" && process == "" {
		if err := requireTerminal("deny"); err != nil {
			return err
		}
		loadedConfig, err := config.Load(configPath)
		if err != nil {
			return err
		}
		list := loadedConfig.Allowlist
		if len(list.Rules) == 0 {
			fmt.Println("Nothing is allowed: there is no Rule to remove.")
			return nil
		}
		chosenRules, err := wizard.ChooseRulesToRemove(list.Rules)
		if err != nil {
			return err
		}
		isChosen = func(rule allowlist.Rule) bool {
			for _, chosenRule := range chosenRules {
				if config.SameRule(rule, chosenRule) {
					return true
				}
			}
			return false
		}
	} else {
		ruleToRemove, err := ruleFromFlags(portText, process)
		if err != nil {
			return err
		}
		isChosen = func(rule allowlist.Rule) bool { return config.SameRule(rule, ruleToRemove) }
	}

	removedRules, err := config.RemoveRules(configPath, isChosen)
	if err != nil {
		return err
	}
	if len(removedRules) == 0 {
		if portText != "" || process != "" {
			return errors.New("no Rule matches exactly these flags: run `tailserve deny` to pick from the list")
		}
		fmt.Println("Nothing chosen: nothing changed.")
		return nil
	}
	for _, rule := range removedRules {
		fmt.Printf("Removed %s.\n", rule)
	}
	printWhenItApplies()
	return nil
}

// ruleFromFlags builds the Rule that --port and --process describe.
func ruleFromFlags(portText, process string) (allowlist.Rule, error) {
	rule := allowlist.Rule{Process: process}
	if portText != "" {
		portRange, err := allowlist.ParsePortRange(portText)
		if err != nil {
			return rule, err
		}
		rule.Port = &portRange
	}
	return rule, nil
}

// printWhenItApplies says whether a running tailserve picks up a config change, or one has to be started.
func printWhenItApplies() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := control.FetchSnapshot(ctx); err == nil {
		fmt.Printf("The running tailserve applies it within %s.\n", daemon.PollInterval)
	} else {
		fmt.Println("Run `tailserve start` to serve the allowed Services.")
	}
}

// requireTerminal fails when stdin is not a terminal, where the wizard of command cannot ask anything.
func requireTerminal(command string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("`tailserve %s` without flags needs a terminal: pass --port or --process instead", command)
	}
	return nil
}

// editConfig opens the config file in $VISUAL or $EDITOR, creating it first if needed.
func editConfig() error {
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	if _, err := config.EnsureFile(configPath); err != nil {
		return err
	}

	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	// The variable may carry arguments, such as "code --wait".
	editorWords := strings.Fields(editor)
	command := exec.Command(editorWords[0], append(editorWords[1:], configPath)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("running %s: %w", editor, err)
	}

	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("the config is invalid, so a running tailserve keeps the previous one: %w", err)
	}
	return nil
}

// describeWithoutTailserve builds the Snapshot `status` shows when tailserve is
// not running: every mapping is outside tailserve, and nothing allowed is Served.
func describeWithoutTailserve(ctx context.Context) (status.Snapshot, error) {
	services, err := (&discovery.LsofScanner{}).Scan(ctx)
	if err != nil {
		return status.Snapshot{}, err
	}

	snapshot := status.Snapshot{}
	list := allowlist.New(nil, nil)
	if snapshot.ConfigPath, err = config.Path(); err != nil {
		snapshot.ConfigError = err.Error()
	} else if loadedConfig, err := config.Load(snapshot.ConfigPath); err != nil {
		snapshot.ConfigError = err.Error()
	} else {
		list = loadedConfig.Allowlist
	}
	snapshot.RuleCount = len(list.Rules)

	serves, err := tailnet.ListServes(ctx, &local.Client{})
	if err != nil {
		snapshot.TailscaleError = err.Error()
	}
	snapshot.Rows = status.Describe(services, list, serves, nil, "")
	status.MarkNotServed(snapshot.Rows, "tailserve is not running")
	return snapshot, nil
}

func printSnapshot(snapshot status.Snapshot, showAll bool) error {
	if snapshot.Running {
		fmt.Printf("tailserve:  running (pid %d)\n", snapshot.PID)
		if snapshot.DashboardURL != "" {
			fmt.Println("Dashboard:  " + snapshot.DashboardURL)
		} else {
			fmt.Println("Dashboard:  not served: " + snapshot.DashboardError)
		}
		if snapshot.DashboardLocalURL != "" {
			fmt.Println("Local:      " + snapshot.DashboardLocalURL)
		}
		if snapshot.DashboardLocalError != "" {
			fmt.Println("            " + snapshot.DashboardLocalError)
		}
	} else {
		fmt.Println("tailserve:  not running")
	}

	if snapshot.ConfigError != "" {
		fmt.Println("Config:     error: " + snapshot.ConfigError)
	} else {
		fmt.Printf("Config:     %s (%d Rules)\n", snapshot.ConfigPath, snapshot.RuleCount)
	}
	if snapshot.TailscaleError != "" {
		fmt.Println("Tailscale:  " + snapshot.TailscaleError)
	} else {
		fmt.Println("Tailscale:  connected")
	}
	fmt.Println()

	var shownRows []status.Row
	hiddenCount := 0
	for _, row := range snapshot.Rows {
		if row.State == status.NotAllowed && !showAll {
			hiddenCount++
			continue
		}
		shownRows = append(shownRows, row)
	}

	if len(shownRows) == 0 {
		fmt.Println("No Service is allowed or served.")
	} else {
		table := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(table, "PORT\tTOOL\tREPOSITORY\tPID\tSTATE\tDETAIL")
		for _, row := range shownRows {
			repositoryName := "-"
			if row.Service.Repository != nil {
				repositoryName = row.Service.Repository.Name
			}
			fmt.Fprintf(table, "%d\t%s\t%s\t%d\t%s\t%s\n",
				row.Service.Port, row.Service.ToolWithProcessName(), repositoryName, row.Service.PID, row.State, row.Detail)
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}

	if hiddenCount > 0 {
		fmt.Printf("\n%d more Services are not allowed by any Rule: run `tailserve status --all` to list them.\n", hiddenCount)
	}
	if snapshot.RuleCount == 0 && snapshot.ConfigError == "" {
		fmt.Println("\nNothing is allowed yet: run `tailserve allow` to pick the Services to serve.")
	}
	return nil
}
