//go:build windows

package main

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestPreferredIconResourceSelectsTraySizedIcon(t *testing.T) {
	iconFile := make([]byte, 6+2*16+7)
	binary.LittleEndian.PutUint16(iconFile[2:4], 1)
	binary.LittleEndian.PutUint16(iconFile[4:6], 2)

	iconFile[6] = 64
	binary.LittleEndian.PutUint32(iconFile[14:18], 3)
	binary.LittleEndian.PutUint32(iconFile[18:22], uint32(6+2*16))
	iconFile[22] = 32
	binary.LittleEndian.PutUint32(iconFile[30:34], 4)
	binary.LittleEndian.PutUint32(iconFile[34:38], uint32(6+2*16+3))
	copy(iconFile[38:], []byte{1, 2, 3, 4, 5, 6, 7})

	selected := preferredIconResource(iconFile)
	if len(selected) != 4 || selected[0] != 4 || selected[3] != 7 {
		t.Fatalf("preferredIconResource() = %v", selected)
	}
}

func TestWindowsTrayStructSizes(t *testing.T) {
	if size := unsafe.Sizeof(trayWindowClass{}); size != 80 {
		t.Fatalf("trayWindowClass size = %d, want 80", size)
	}
	if size := unsafe.Sizeof(trayMessage{}); size != 48 {
		t.Fatalf("trayMessage size = %d, want 48", size)
	}
	if size := unsafe.Sizeof(trayNotifyIconData{}); size != 976 {
		t.Fatalf("trayNotifyIconData size = %d, want 976", size)
	}
}
