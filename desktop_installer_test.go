package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsInstallerPreservesExistingInstallDirectory(t *testing.T) {
	template, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatal(err)
	}
	content := string(template)
	required := []string{
		`ReadRegStr $0 ${ROOT} "${UNINST_KEY}" "InstallLocation"`,
		`ReadRegStr $0 ${ROOT} "${UNINST_KEY}" "UninstallString"`,
		`!insertmacro wails.restoreInstallDir HKCU`,
		`WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`,
		`WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`,
	}
	for _, expected := range required {
		if !strings.Contains(content, expected) {
			t.Fatalf("Windows installer template is missing %q", expected)
		}
	}
}
