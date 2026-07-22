//go:build windows

package executil

import (
	"context"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackgroundCommandsSuppressWindowsConsole(t *testing.T) {
	tests := map[string]*exec.Cmd{
		"command":         Command("ffmpeg.exe", "-version"),
		"command context": CommandContext(context.Background(), "ffprobe.exe", "-version"),
	}
	for name, cmd := range tests {
		t.Run(name, func(t *testing.T) {
			if cmd.SysProcAttr == nil {
				t.Fatal("SysProcAttr is nil")
			}
			if !cmd.SysProcAttr.HideWindow {
				t.Fatal("HideWindow is false")
			}
			if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
				t.Fatal("CREATE_NO_WINDOW is not set")
			}
		})
	}
}

func TestVisibleCommandKeepsDefaultWindowsAttributes(t *testing.T) {
	if cmd := VisibleCommand("explorer.exe"); cmd.SysProcAttr != nil {
		t.Fatalf("SysProcAttr = %#v, want nil", cmd.SysProcAttr)
	}
}
