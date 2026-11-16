package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebPageOf(t *testing.T) {
	tests := []struct {
		remote, webURL, owner, name string
	}{
		{"git@github.com:marcomontalbano/tailserve.git", "https://github.com/marcomontalbano/tailserve", "marcomontalbano", "tailserve"},
		{"ssh://git@github.com:22/marcomontalbano/tailserve.git", "https://github.com/marcomontalbano/tailserve", "marcomontalbano", "tailserve"},
		{"https://user:secret-token@github.com/marcomontalbano/tailserve.git", "https://github.com/marcomontalbano/tailserve", "marcomontalbano", "tailserve"},
		{"https://gitlab.com/group/subgroup/app", "https://gitlab.com/group/subgroup/app", "group/subgroup", "app"},
	}
	for _, test := range tests {
		webURL, owner, name, ok := webPageOf(test.remote)
		if !ok || webURL != test.webURL || owner != test.owner || name != test.name {
			t.Errorf("webPageOf(%q) = %q, %q, %q, %v; want %q, %q, %q", test.remote, webURL, owner, name, ok, test.webURL, test.owner, test.name)
		}
	}
	for _, remote := range []string{"", "/local/path/repo.git", "github.com"} {
		if _, _, _, ok := webPageOf(remote); ok {
			t.Errorf("webPageOf(%q) found a web page", remote)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRepository(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "tailserve")
	writeFile(t, filepath.Join(clone, ".git", "config"), `[core]
	bare = false
[remote "upstream"]
	url = git@github.com:someone-else/tailserve.git
[remote "origin"]
	url = git@github.com:marcomontalbano/tailserve.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`)
	packageFolder := filepath.Join(clone, "internal", "dashboard")
	if err := os.MkdirAll(packageFolder, 0o755); err != nil {
		t.Fatal(err)
	}

	repository := findRepository(packageFolder)
	want := Repository{Root: clone, Owner: "marcomontalbano", Name: "tailserve", WebURL: "https://github.com/marcomontalbano/tailserve"}
	if repository == nil || *repository != want {
		t.Fatalf("findRepository() = %+v, want %+v", repository, want)
	}

	worktree := filepath.Join(root, "tailserve-fix")
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+filepath.Join(clone, ".git", "worktrees", "fix")+"\n")
	writeFile(t, filepath.Join(clone, ".git", "worktrees", "fix", "commondir"), "../..\n")
	if repository := findRepository(worktree); repository == nil || repository.Name != "tailserve" || repository.Root != worktree {
		t.Errorf("findRepository() in a worktree = %+v, want tailserve rooted at the worktree", repository)
	}

	withoutRemote := filepath.Join(root, "scratch")
	writeFile(t, filepath.Join(withoutRemote, ".git", "config"), "[core]\n")
	if repository := findRepository(withoutRemote); repository == nil || repository.Name != "scratch" || repository.WebURL != "" {
		t.Errorf("findRepository() without a remote = %+v, want the folder name and no web page", repository)
	}

	if repository := findRepository(t.TempDir()); repository != nil {
		t.Errorf("findRepository() outside any repository = %+v, want nil", repository)
	}
}

func TestToolOf(t *testing.T) {
	tests := []struct {
		arguments []string
		want      string
	}{
		{[]string{"node", "/p/docs/node_modules/.bin/../.pnpm/astro@7.4.2_x/node_modules/astro/bin/astro.mjs", "dev", "--force"}, "astro dev"},
		{[]string{"node", "/p/docs/node_modules/.pnpm/astro@7.4.2_x/node_modules/astro/bin/astro.mjs", "preview", "--port", "4329"}, "astro preview"},
		{[]string{"node", "/p/index/node_modules/.bin/../vite/bin/vite.js", "--port", "5182"}, "vite"},
		{[]string{"node", "/p/docs/node_modules/.bin/../storybook/dist/bin/dispatcher.js", "dev", "--port", "5180"}, "storybook dev"},
		{[]string{"node", "/p/app/node_modules/@scope/cli/bin.js", "serve"}, "@scope/cli serve"},
		{[]string{"python3", "-m", "http.server", "8000"}, ""},
		{[]string{"node", "server.js"}, ""},
	}
	for _, test := range tests {
		if got := toolOf(test.arguments); got != test.want {
			t.Errorf("toolOf(%q) = %q, want %q", test.arguments, got, test.want)
		}
	}
}
