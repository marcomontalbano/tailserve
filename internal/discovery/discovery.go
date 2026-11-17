// Package discovery finds the Services listening on this machine.
package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Service is one process listening on one TCP port.
type Service struct {
	Port        uint16
	ProcessName string
	PID         int
	// Addresses the Service listens on, such as "127.0.0.1", "[::1]" or "*" for every interface.
	Addresses []string
	// Folder is the working folder of the process, such as the project a dev server runs in; empty when unknown or "/".
	Folder string
	// Repository holds Folder; nil outside any git repository.
	Repository *Repository
	// Tool is what the process runs, with its subcommand, such as "astro dev"; empty when unknown.
	Tool string
}

// ToolName returns the Tool, or the Process name when the Tool is unknown.
func (s Service) ToolName() string {
	if s.Tool != "" {
		return s.Tool
	}
	return s.ProcessName
}

// ToolWithProcessName returns the Tool followed by the Process name in brackets, such as
// "astro dev (node)", or only the Process name when the Tool is unknown or the same.
func (s Service) ToolWithProcessName() string {
	if s.Tool == "" || strings.EqualFold(s.Tool, s.ProcessName) {
		return s.ProcessName
	}
	return s.Tool + " (" + s.ProcessName + ")"
}

// PathInRepository returns Folder relative to the Repository root, such as
// "packages/docs"; empty at the root or outside a Repository.
func (s Service) PathInRepository() string {
	if s.Repository == nil {
		return ""
	}
	relativePath, err := filepath.Rel(s.Repository.Root, s.Folder)
	if err != nil || relativePath == "." {
		return ""
	}
	return relativePath
}

// ShortFolder returns Folder with the home folder written as "~".
func (s Service) ShortFolder() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return s.Folder
	}
	if s.Folder == home {
		return "~"
	}
	if rest, isInHome := strings.CutPrefix(s.Folder, home+string(filepath.Separator)); isInHome {
		return filepath.Join("~", rest)
	}
	return s.Folder
}

// FolderName returns the last element of Folder, such as "website", or "" when the folder is unknown.
func (s Service) FolderName() string {
	if s.Folder == "" {
		return ""
	}
	return filepath.Base(s.Folder)
}

// Scanner lists the Services currently listening on this machine, sorted by port.
type Scanner interface {
	Scan(ctx context.Context) ([]Service, error)
}
