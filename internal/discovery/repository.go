package discovery

import (
	"bufio"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Repository is the git repository that holds a Service's Folder.
type Repository struct {
	// Root is the folder that holds .git.
	Root string
	// Owner is the account or group of the origin remote, such as "marcomontalbano"; empty without a remote.
	Owner string
	// Name is the repository name of the origin remote, or the Root folder name without a remote.
	Name string
	// WebURL is the origin remote as a web page, such as "https://github.com/marcomontalbano/tailserve"; empty without a remote.
	WebURL string
}

// findRepository returns the git repository holding folder, or nil when there is none.
func findRepository(folder string) *Repository {
	if folder == "" {
		return nil
	}
	for current := folder; ; current = filepath.Dir(current) {
		gitPath := filepath.Join(current, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			repository := &Repository{Root: current, Name: filepath.Base(current)}
			if remoteURL := originURL(gitPath, info.IsDir()); remoteURL != "" {
				if webURL, owner, name, ok := webPageOf(remoteURL); ok {
					repository.WebURL, repository.Owner, repository.Name = webURL, owner, name
				}
			}
			return repository
		}
		if parent := filepath.Dir(current); parent == current {
			return nil
		}
	}
}

// originURL reads the URL of the origin remote from the git config behind gitPath,
// which is a folder in a clone and a file pointing to the real one in a worktree.
func originURL(gitPath string, isFolder bool) string {
	gitFolder := gitPath
	if !isFolder {
		content, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		target, found := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir: ")
		if !found {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(gitPath), target)
		}
		gitFolder = target
		// A worktree keeps its config in the main repository, named by commondir.
		if commonFolder, err := os.ReadFile(filepath.Join(gitFolder, "commondir")); err == nil {
			commonPath := strings.TrimSpace(string(commonFolder))
			if !filepath.IsAbs(commonPath) {
				commonPath = filepath.Join(gitFolder, commonPath)
			}
			gitFolder = commonPath
		}
	}

	config, err := os.ReadFile(filepath.Join(gitFolder, "config"))
	if err != nil {
		return ""
	}
	isInOrigin := false
	scanner := bufio.NewScanner(bytes.NewReader(config))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			isInOrigin = line == `[remote "origin"]`
			continue
		}
		if !isInOrigin {
			continue
		}
		if key, value, found := strings.Cut(line, "="); found && strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// webPageOf turns a git remote into the repository's https page, dropping any
// credentials: "git@github.com:owner/repo.git" becomes "https://github.com/owner/repo".
func webPageOf(remoteURL string) (webURL, owner, name string, ok bool) {
	var host, path string
	if strings.Contains(remoteURL, "://") {
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", "", "", false
		}
		host, path = parsed.Hostname(), parsed.Path
	} else {
		// The scp-like form: [user@]host:path.
		hostPart, pathPart, found := strings.Cut(remoteURL, ":")
		if !found {
			return "", "", "", false
		}
		if _, afterAt, hasUser := strings.Cut(hostPart, "@"); hasUser {
			hostPart = afterAt
		}
		host, path = hostPart, pathPart
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name = filepath.Dir(path), filepath.Base(path)
	if host == "" || owner == "." || owner == "" || name == "" {
		return "", "", "", false
	}
	return "https://" + host + "/" + path, owner, name, true
}
