//go:build windows

package main

import "testing"

func TestInstalledPathMatches(t *testing.T) {
	executable := `C:\Users\Test\AppData\Local\Programs\NyaMediaMetadataTool\NyaMediaMetadataTool.exe`
	if !installedPathMatches(
		executable,
		`C:\Users\Test\AppData\Local\Programs\NyaMediaMetadataTool`,
		"",
	) {
		t.Fatal("install location did not match its executable")
	}
	if !installedPathMatches(
		executable,
		"",
		`"C:\Users\Test\AppData\Local\Programs\NyaMediaMetadataTool\NyaMediaMetadataTool.exe",0`,
	) {
		t.Fatal("display icon did not match its executable")
	}
	if installedPathMatches(executable, `D:\Portable\NyaMediaMetadataTool`, "") {
		t.Fatal("unrelated install location matched the executable")
	}
}
