package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	gitBuild := &debug.BuildInfo{
		GoVersion: "go1.27.4",
		Main:      debug.Module{Version: "v0.1.1-0.20261118000431-a2307e4abcde"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "a2307e4abcdef"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	goInstall := &debug.BuildInfo{GoVersion: "go1.27.4", Main: debug.Module{Version: "v0.1.0"}}
	localBuild := &debug.BuildInfo{GoVersion: "go1.27.4", Main: debug.Module{Version: "(devel)"}}

	tests := []struct {
		name                        string
		linkedVersion, linkedCommit string
		recorded                    *debug.BuildInfo
		want                        Info
	}{
		{"build flags win", "v0.2.0", "f00d", gitBuild, Info{Version: "0.2.0", Commit: "f00d", GoVersion: "go1.27.4"}},
		{"go install of a tag", "", "", goInstall, Info{Version: "0.1.0", GoVersion: "go1.27.4"}},
		{"build from a modified checkout", "", "", gitBuild, Info{Version: "0.1.1-0.20261118000431-a2307e4abcde", Commit: "a2307e4abcdef-dirty", GoVersion: "go1.27.4"}},
		{"unversioned build", "", "", localBuild, Info{Version: "dev", GoVersion: "go1.27.4"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolve(test.linkedVersion, test.linkedCommit, test.recorded); got != test.want {
				t.Errorf("resolve() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	info := Info{Version: "0.1.0", Commit: "a2307e4", GoVersion: "go1.27.4"}
	want := "0.1.0\n  commit: a2307e4\n  go version: go1.27.4\n"
	if got := info.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := (Info{Version: "dev", GoVersion: "go1.27.4"}).String(); got != "dev\n  go version: go1.27.4\n" {
		t.Errorf("String() without a commit = %q", got)
	}
}
