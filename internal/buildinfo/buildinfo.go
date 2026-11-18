// Package buildinfo reports which build of tailserve is running.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Info is the version, commit and Go version of a tailserve binary.
type Info struct {
	// Version is a release such as "0.1.0", a Go pseudo-version for an untagged build, or "dev" when unknown.
	Version string
	// Commit is the git commit the binary was built from; empty when unknown.
	Commit    string
	GoVersion string
}

// Current returns the Info of this binary. linkedVersion and linkedCommit are
// what the build set with -ldflags; empty ones fall back to what Go recorded.
func Current(linkedVersion, linkedCommit string) Info {
	// ReadBuildInfo returns nil when the binary carries no build information.
	recorded, _ := debug.ReadBuildInfo()
	return resolve(linkedVersion, linkedCommit, recorded)
}

// resolve picks each value from the build flags first, then from what Go recorded in recorded, which may be nil.
func resolve(linkedVersion, linkedCommit string, recorded *debug.BuildInfo) Info {
	info := Info{Version: linkedVersion, Commit: linkedCommit, GoVersion: runtime.Version()}
	if recorded == nil {
		info.Version = cleanVersion(info.Version)
		return info
	}

	if info.Version == "" {
		info.Version = recorded.Main.Version
	}
	if info.Commit == "" {
		isModified := false
		for _, setting := range recorded.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Commit = setting.Value
			case "vcs.modified":
				isModified = setting.Value == "true"
			}
		}
		if info.Commit != "" && isModified {
			info.Commit += "-dirty"
		}
	}
	if recorded.GoVersion != "" {
		info.GoVersion = recorded.GoVersion
	}
	info.Version = cleanVersion(info.Version)
	return info
}

// cleanVersion drops the leading "v" of a tag, as Tailscale prints its version,
// and turns the "(devel)" Go records for an unversioned build into "dev".
func cleanVersion(version string) string {
	if version == "" || version == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(version, "v")
}

// String formats the Info the way `tailserve version` prints it: the version
// alone on the first line, so scripts can read it, then the details indented.
func (info Info) String() string {
	var text strings.Builder
	fmt.Fprintln(&text, info.Version)
	if info.Commit != "" {
		fmt.Fprintf(&text, "  commit: %s\n", info.Commit)
	}
	fmt.Fprintf(&text, "  go version: %s\n", info.GoVersion)
	return text.String()
}
