//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ============================================================
// Windows 控制台相关：让双击运行的窗口有内容、不容易误关
// ============================================================

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleMode   = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode   = kernel32.NewProc("SetConsoleMode")
	procSetConsoleTitleW = kernel32.NewProc("SetConsoleTitleW")
	procGetStdHandle     = kernel32.NewProc("GetStdHandle")
	procSetConsoleText   = kernel32.NewProc("SetConsoleTextAttribute")
	procMessageBox       = user32.NewProc("MessageBoxW")
)

const (
	stdOutputHandle = ^uintptr(10) // -11
	stdInputHandle  = ^uintptr(9)  // -10

	fgDefault = 7
	fgGray    = 8
	fgGreen   = 10
	fgCyan    = 11
	fgRed     = 12
	fgYellow  = 14

	enableQuickEdit = 0x0040 // 快速编辑模式，会让输出冻结

	mbOK        = 0x00000000
	mbIconError = 0x00000010
	mbTopmost   = 0x00040000
)

// isConsoleAvailable Windows 下用 GetConsoleWindow 判断是否有控制台
func isConsoleAvailable() bool {
	h, _, _ := procGetConsoleWindow.Call()
	return h != 0
}

// initConsole 设置标题并关闭「快速编辑」。
// 快速编辑开着时，用户在窗口里点一下就会进入选择状态，
// 所有输出立刻冻结，看起来就像卡死了。
func initConsole(title string) {
	if p, err := syscall.UTF16PtrFromString(title); err == nil {
		procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
	}
	h, _, _ := procGetStdHandle.Call(stdInputHandle)
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
		if mode&enableQuickEdit != 0 {
			procSetConsoleMode.Call(h, uintptr(mode&^enableQuickEdit))
		}
	}
}

// colorln 带颜色输出一行，输出后恢复默认色
func colorln(c uintptr, s string) {
	h, _, _ := procGetStdHandle.Call(stdOutputHandle)
	procSetConsoleText.Call(h, c)
	fmt.Println(s)
	procSetConsoleText.Call(h, fgDefault)
}

// printBanner 打印启动面板，让用户一眼看到「程序在跑」和「地址在哪」
func printBanner(url, dir, dbPath string, newDB bool) {
	bar := strings.Repeat("=", 58)
	fmt.Println()
	colorln(fgCyan, "  "+bar)
	colorln(fgCyan, "   "+appTitle+"   "+buildInfo())
	colorln(fgCyan, "  "+bar)
	fmt.Println()
	fmt.Println("   程序已在后台运行，界面用浏览器访问：")
	fmt.Println()
	colorln(fgGreen, "       "+url)
	fmt.Println()
	if newDB {
		colorln(fgYellow, "   数据库："+dbPath+"  （首次运行，已新建空库）")
	} else {
		colorln(fgGray, "   数据库："+dbPath)
	}
	colorln(fgGray, "   程序目录："+dir)
	fmt.Println()
	colorln(fgYellow, "   提示：浏览器没自动打开时，复制上面的地址粘贴到浏览器即可")
	colorln(fgYellow, "   退出：按 Ctrl+C 正常退出；直接关闭本窗口也会停止服务")
	fmt.Println()
	colorln(fgGray, "   ------------------------------------------------------------")
	colorln(fgGray, "   这个窗口是程序的运行窗口，请不要关闭，用完再关。")
	colorln(fgGray, "   ------------------------------------------------------------")
	fmt.Println()
}

// printExitHint 正常退出提示
func printExitHint() {
	fmt.Println()
	colorln(fgGray, "   服务已停止，本窗口可以关闭了。")
}

// waitClose 等待关闭：按回车立即关闭，或到点自动关闭
func waitClose(seconds int, hint string) {
	if hint != "" {
		fmt.Println()
		colorln(fgGray, "   "+hint)
	}
	done := make(chan struct{})
	go func() {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Duration(seconds) * time.Second):
	}
}

// holdOnExit 出错退出前停住，避免窗口一闪而过看不到原因
func holdOnExit(seconds int) {
	waitClose(seconds, fmt.Sprintf("窗口将在 %d 秒后自动关闭，按回车可立即关闭。", seconds))
}

// openBrowserImpl 用 rundll32 打开默认浏览器（不弹黑色命令窗）
func openBrowserImpl(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// showErrorBox 用 Win32 MessageBoxW 弹错误框，无控制台模式下也能看到
func showErrorBox(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	_, _, _ = procMessageBox.Call(
		0,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		uintptr(mbOK|mbIconError|mbTopmost),
	)
}
