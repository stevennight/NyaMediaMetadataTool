//go:build !windows

package executil

import "os/exec"

func configureBackground(_ *exec.Cmd) {}
