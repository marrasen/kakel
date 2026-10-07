//go:build windows

package single

import "golang.org/x/sys/windows"

// alive reports whether a process numbered pid is running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// STILL_ACTIVE.
	return code == 259
}

var allowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// letToFront lets the process numbered pid bring its windows to the
// front. Windows lets only the program the user just started do that,
// so a kakel handing over its command line passes the right on to the
// one running, or the window that one opens stays behind the taskbar
// or the window last used.
func letToFront(pid int) {
	if allowSetForegroundWindow.Find() == nil {
		_, _, _ = allowSetForegroundWindow.Call(uintptr(pid))
	}
}
