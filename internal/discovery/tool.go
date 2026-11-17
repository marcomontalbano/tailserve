package discovery

import (
	"path/filepath"
	"regexp"
	"strings"
)

// subcommandPattern matches a word like "dev" or "preview", not a flag, a path or a value.
var subcommandPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// toolOf names the package a command line runs from node_modules, followed by
// its subcommand when there is one: "astro dev" for `node …/node_modules/astro/bin/astro.mjs dev --force`.
// It returns "" when the command runs nothing from node_modules.
func toolOf(arguments []string) string {
	for index, argument := range arguments {
		if index == 0 {
			continue
		}
		packageName := packageInNodeModules(argument)
		if packageName == "" {
			continue
		}
		if index+1 < len(arguments) && subcommandPattern.MatchString(arguments[index+1]) {
			return packageName + " " + arguments[index+1]
		}
		return packageName
	}
	return ""
}

// packageInNodeModules returns the package that path points into, after its last
// node_modules folder: "vite" for ".../node_modules/.bin/../vite/bin/vite.js".
func packageInNodeModules(path string) string {
	const marker = "/node_modules/"
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	markerIndex := strings.LastIndex(cleanPath, marker)
	if markerIndex < 0 {
		return ""
	}
	parts := strings.Split(cleanPath[markerIndex+len(marker):], "/")
	if len(parts) < 2 || parts[0] == ".bin" || parts[0] == ".pnpm" {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) >= 3 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
