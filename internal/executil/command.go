package executil

import (
	"context"
	"os/exec"
)

// Command creates a background process. On Windows, its console window is
// suppressed; other platforms retain the standard os/exec behavior.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	configureBackground(cmd)
	return cmd
}

// CommandContext is Command with context cancellation.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configureBackground(cmd)
	return cmd
}

// VisibleCommand creates a process that is expected to display user-facing UI.
func VisibleCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
