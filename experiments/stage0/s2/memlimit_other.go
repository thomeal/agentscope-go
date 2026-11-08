//go:build !windows

package s2

import "os/exec"

// startCapped starts cmd. The memory cap is Windows-only, where the runs take place.
func startCapped(cmd *exec.Cmd) (func(), error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {}, nil
}
