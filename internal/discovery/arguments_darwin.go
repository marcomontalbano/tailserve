package discovery

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// processArguments returns the command line of the process pid, starting with
// the program, exactly as it was started; it works for the current user's processes.
func processArguments(pid int) ([]string, error) {
	// The buffer holds argc, the executable path, NUL padding, then argc NUL-terminated arguments.
	buffer, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	if len(buffer) < 4 {
		return nil, errors.New("kern.procargs2: buffer too short")
	}
	argumentCount := int(binary.LittleEndian.Uint32(buffer[:4]))
	rest := buffer[4:]

	executableEnd := bytes.IndexByte(rest, 0)
	if executableEnd < 0 {
		return nil, errors.New("kern.procargs2: no executable path")
	}
	rest = bytes.TrimLeft(rest[executableEnd:], "\x00")

	arguments := make([]string, 0, argumentCount)
	for len(arguments) < argumentCount && len(rest) > 0 {
		argumentEnd := bytes.IndexByte(rest, 0)
		if argumentEnd < 0 {
			argumentEnd = len(rest)
		}
		arguments = append(arguments, string(rest[:argumentEnd]))
		rest = rest[min(argumentEnd+1, len(rest)):]
	}
	return arguments, nil
}
