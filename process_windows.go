//go:build windows

package harness

import (
	"os"
	"os/exec"
)

func prepareProcessGroup(_ *exec.Cmd) {}

func terminateProcessGroup(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	return process.Kill()
}
