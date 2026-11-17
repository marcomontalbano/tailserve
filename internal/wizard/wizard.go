// Package wizard asks, in the terminal, which Rules to add to or remove from the Allowlist.
package wizard

import (
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/huh"
	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/marcomontalbano/tailserve/internal/status"
)

// ErrCancelled means the person left the wizard without choosing.
var ErrCancelled = errors.New("cancelled")

// The ways a new Rule can match a Service: its port and Process name together, its port alone, or its Process name alone.
const (
	matchByPortAndProcess = "port and process"
	matchByPort           = "port"
	matchByProcess        = "process"
)

// ChooseRulesToAdd asks which of the not yet Served Services in rows to serve,
// and whether each one is matched by its port and Process name, its port, or its Process name.
func ChooseRulesToAdd(rows []status.Row) ([]allowlist.Rule, error) {
	var options []huh.Option[int]
	for index, row := range rows {
		label := fmt.Sprintf("%s on %d", row.Service.ToolWithProcessName(), row.Service.Port)
		if folder := row.Service.ShortFolder(); folder != "" {
			label += " · " + folder
		}
		if row.State == status.Locked {
			label += " · locked"
		}
		options = append(options, huh.NewOption(label, index))
	}

	// One form holds every question: a follow-up group per Service, shown only
	// when that Service is chosen, so the terminal is never handed between forms.
	var chosenIndexes []int
	groups := []*huh.Group{huh.NewGroup(huh.NewMultiSelect[int]().
		Title("Which Services should tailserve serve?").
		Description("Space selects, Enter confirms.").
		Options(options...).
		Height(min(len(options)+3, 15)).
		Value(&chosenIndexes))}

	matches := make([]string, len(rows))
	lockedPortConfirmations := make([]bool, len(rows))
	for index, row := range rows {
		isNotChosen := func() bool { return !slices.Contains(chosenIndexes, index) }
		matches[index] = matchByPortAndProcess

		if row.State == status.Locked {
			groups = append(groups, huh.NewGroup(huh.NewConfirm().
				Title(fmt.Sprintf("Port %d is locked. Serve it anyway?", row.Service.Port)).
				Description("Locked ports hold debuggers, databases and remote access. Only a Rule naming this exact port serves it.").
				Value(&lockedPortConfirmations[index])).WithHideFunc(isNotChosen))
			continue
		}
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title(fmt.Sprintf("How should the Rule match %s on %d?", row.Service.ProcessName, row.Service.Port)).
			Options(
				huh.NewOption(fmt.Sprintf("Port %d, only when %s listens on it", row.Service.Port, row.Service.ProcessName), matchByPortAndProcess),
				huh.NewOption(fmt.Sprintf("Port %d, whatever process listens on it", row.Service.Port), matchByPort),
				huh.NewOption(fmt.Sprintf("Any %s process, on any port", row.Service.ProcessName), matchByProcess),
			).
			Value(&matches[index])).WithHideFunc(isNotChosen))
	}

	if err := run(groups...); err != nil {
		return nil, err
	}

	var rules []allowlist.Rule
	for _, index := range chosenIndexes {
		row := rows[index]
		port := &allowlist.PortRange{First: row.Service.Port, Last: row.Service.Port}
		switch {
		case row.State == status.Locked:
			// The Rule names this exact port, which unlocks it, and stays tied to the program that was seen on it.
			if lockedPortConfirmations[index] {
				rules = append(rules, allowlist.Rule{Port: port, Process: row.Service.ProcessName})
			}
		case matches[index] == matchByProcess:
			rules = append(rules, allowlist.Rule{Process: row.Service.ProcessName})
		case matches[index] == matchByPort:
			rules = append(rules, allowlist.Rule{Port: port})
		default:
			rules = append(rules, allowlist.Rule{Port: port, Process: row.Service.ProcessName})
		}
	}
	return rules, nil
}

// ChooseRulesToRemove asks which of rules to remove from the Allowlist.
func ChooseRulesToRemove(rules []allowlist.Rule) ([]allowlist.Rule, error) {
	var options []huh.Option[int]
	for index, rule := range rules {
		label := rule.String()
		if rule.Note != "" {
			label += " · " + rule.Note
		}
		options = append(options, huh.NewOption(label, index))
	}

	var chosenIndexes []int
	err := run(huh.NewGroup(huh.NewMultiSelect[int]().
		Title("Which Rules should be removed?").
		Description("Space selects, Enter confirms.").
		Options(options...).
		Height(min(len(options)+3, 15)).
		Value(&chosenIndexes)))
	if err != nil {
		return nil, err
	}

	chosenRules := make([]allowlist.Rule, 0, len(chosenIndexes))
	for _, index := range chosenIndexes {
		chosenRules = append(chosenRules, rules[index])
	}
	return chosenRules, nil
}

// run shows groups as one form, turning Ctrl-C into ErrCancelled.
func run(groups ...*huh.Group) error {
	err := huh.NewForm(groups...).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}
