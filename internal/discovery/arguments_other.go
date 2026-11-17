//go:build !darwin

package discovery

import "errors"

// processArguments is only implemented on macOS; elsewhere Services have no Tool.
func processArguments(int) ([]string, error) {
	return nil, errors.New("reading process arguments is only supported on macOS")
}
