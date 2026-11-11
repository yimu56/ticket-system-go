//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// version 构建版本号（见 version.go）

// isConsoleAvailable Windows 下用 GetConsoleWindow 判断是否有控制台
func isConsoleAvailable() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	h, _, _ := procGetConsoleWindow.Call()
	return h != 0
}

// openBrowserImpl 用 rundll32 打开默认浏览器（不弹黑色命令窗）
func openBrowserImpl(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// showErrorBox 用 Win32 MessageBoxW 弹错误框，无控制台模式下也能看到
func showErrorBox(title, msg string) {
	const (
		mbOK            = 0x00000000
		mbIconError     = 0x00000010
		mbSetForeground = 0x00010000
	)
	user32 := syscall.NewLazyDLL("user32.dll")
	procMessageBox := user32.NewProc("MessageBoxW")

	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	// 使用拿到的错误码：失败时也只是弹窗失败，不影响主流程
	_, _, _ = procMessageBox.Call(
		0,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		uintptr(mbOK|mbIconError|mbSetForeground),
	)
}
