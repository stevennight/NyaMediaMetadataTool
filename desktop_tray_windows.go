//go:build windows

package main

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayCallbackMessage = 0x8001
	wmClose             = 0x0010
	wmDestroy           = 0x0002
	wmLeftButtonDouble  = 0x0203
	wmRightButtonUp     = 0x0205

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmNonotify    = 0x0080
	tpmReturnCmd   = 0x0100

	trayMenuOpen = 1001
	trayMenuQuit = 1002
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	shell32                    = windows.NewLazySystemDLL("shell32.dll")
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassEx        = user32.NewProc("RegisterClassExW")
	procCreateWindowEx         = user32.NewProc("CreateWindowExW")
	procDefWindowProc          = user32.NewProc("DefWindowProcW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetMessage             = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessage        = user32.NewProc("DispatchMessageW")
	procPostMessage            = user32.NewProc("PostMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenu             = user32.NewProc("AppendMenuW")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procLoadIcon               = user32.NewProc("LoadIconW")
	procCreateIconFromResource = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procGetModuleHandle        = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIcon        = shell32.NewProc("Shell_NotifyIconW")

	trayWindowProc = syscall.NewCallback(desktopTrayWindowProc)
	trayWindows    sync.Map
)

//go:embed build/windows/icon.ico
var desktopTrayIcon []byte

type trayPoint struct {
	X int32
	Y int32
}

type trayMessage struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   trayPoint
	Private uint32
}

type trayWindowClass struct {
	Size        uint32
	Style       uint32
	WindowProc  uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
	SmallIcon   uintptr
}

type trayNotifyIconData struct {
	Size             uint32
	Window           uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	VersionOrTimeout uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             [16]byte
	BalloonIcon      uintptr
}

type windowsDesktopTray struct {
	window     uintptr
	icon       uintptr
	customIcon bool
	onOpen     func()
	onQuit     func()
	done       chan struct{}
	closeOnce  sync.Once
}

func desktopTraySupported() bool {
	return true
}

func newDesktopTray(onOpen func(), onQuit func()) (desktopTray, error) {
	ready := make(chan error, 1)
	tray := &windowsDesktopTray{onOpen: onOpen, onQuit: onQuit, done: make(chan struct{})}
	go tray.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return tray, nil
}

func (t *windowsDesktopTray) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)

	instance, _, _ := procGetModuleHandle.Call(0)
	className, _ := windows.UTF16PtrFromString("NyaMediaMetadataToolDesktopTray")
	class := trayWindowClass{
		Size:       uint32(unsafe.Sizeof(trayWindowClass{})),
		WindowProc: trayWindowProc,
		Instance:   instance,
		ClassName:  className,
	}
	if result, _, callErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); result == 0 && !errors.Is(callErr, windows.ERROR_CLASS_ALREADY_EXISTS) {
		ready <- fmt.Errorf("register tray window: %w", callErr)
		return
	}

	windowName, _ := windows.UTF16PtrFromString(desktopProductName)
	window, _, callErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, instance, 0,
	)
	if window == 0 {
		ready <- fmt.Errorf("create tray window: %w", callErr)
		return
	}
	t.window = window
	t.icon, t.customIcon = loadDesktopTrayIcon()
	trayWindows.Store(window, t)

	data := t.notifyData()
	if result, _, callErr := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data))); result == 0 {
		trayWindows.Delete(window)
		procDestroyWindow.Call(window)
		if t.customIcon {
			procDestroyIcon.Call(t.icon)
		}
		ready <- fmt.Errorf("add tray icon: %w", callErr)
		return
	}
	ready <- nil

	var message trayMessage
	for {
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func (t *windowsDesktopTray) notifyData() trayNotifyIconData {
	data := trayNotifyIconData{
		Size:            uint32(unsafe.Sizeof(trayNotifyIconData{})),
		Window:          t.window,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: trayCallbackMessage,
		Icon:            t.icon,
	}
	tip, _ := windows.UTF16FromString(desktopProductName + " - 后台运行中")
	copy(data.Tip[:], tip)
	return data
}

func (t *windowsDesktopTray) Close() error {
	t.closeOnce.Do(func() {
		procPostMessage.Call(t.window, wmClose, 0, 0)
	})
	<-t.done
	return nil
}

func desktopTrayWindowProc(window uintptr, message uint32, wParam uintptr, lParam uintptr) uintptr {
	value, exists := trayWindows.Load(window)
	if !exists {
		result, _, _ := procDefWindowProc.Call(window, uintptr(message), wParam, lParam)
		return result
	}
	tray := value.(*windowsDesktopTray)
	switch message {
	case trayCallbackMessage:
		switch uint32(lParam) & 0xffff {
		case wmLeftButtonDouble:
			go tray.onOpen()
		case wmRightButtonUp:
			tray.showMenu()
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(window)
		return 0
	case wmDestroy:
		data := tray.notifyData()
		procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
		trayWindows.Delete(window)
		if tray.customIcon {
			procDestroyIcon.Call(tray.icon)
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(window, uintptr(message), wParam, lParam)
	return result
}

func (t *windowsDesktopTray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	openLabel, _ := windows.UTF16PtrFromString("打开 " + desktopProductName)
	quitLabel, _ := windows.UTF16PtrFromString("退出")
	procAppendMenu.Call(menu, mfString, trayMenuOpen, uintptr(unsafe.Pointer(openLabel)))
	procAppendMenu.Call(menu, mfSeparator, 0, 0)
	procAppendMenu.Call(menu, mfString, trayMenuQuit, uintptr(unsafe.Pointer(quitLabel)))

	var point trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	procSetForegroundWindow.Call(t.window)
	command, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmNonotify|tpmReturnCmd, uintptr(point.X), uintptr(point.Y), 0, t.window, 0)
	switch command {
	case trayMenuOpen:
		go t.onOpen()
	case trayMenuQuit:
		go t.onQuit()
	}
}

func loadDesktopTrayIcon() (uintptr, bool) {
	if data := preferredIconResource(desktopTrayIcon); len(data) > 0 {
		icon, _, _ := procCreateIconFromResource.Call(
			uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, 0, 0, 0,
		)
		if icon != 0 {
			return icon, true
		}
	}
	icon, _, _ := procLoadIcon.Call(0, 32512)
	return icon, false
}

func preferredIconResource(iconFile []byte) []byte {
	if len(iconFile) < 6 || binary.LittleEndian.Uint16(iconFile[0:2]) != 0 || binary.LittleEndian.Uint16(iconFile[2:4]) != 1 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(iconFile[4:6]))
	bestScore := -1
	var best []byte
	for index := 0; index < count; index++ {
		entry := 6 + index*16
		if entry+16 > len(iconFile) {
			break
		}
		width := int(iconFile[entry])
		if width == 0 {
			width = 256
		}
		size := int(binary.LittleEndian.Uint32(iconFile[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(iconFile[entry+12 : entry+16]))
		if size <= 0 || offset < 0 || offset+size > len(iconFile) {
			continue
		}
		score := width
		if width == 32 {
			score += 1000
		}
		if score > bestScore {
			bestScore = score
			best = iconFile[offset : offset+size]
		}
	}
	return best
}
