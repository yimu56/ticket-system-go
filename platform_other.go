//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func isConsoleAvailable() bool { return true }

// initConsole 非 Windows 无需处理
func initConsole(title string) {}

func colorln(c uintptr, s string) { fmt.Println(s) }

const (
	fgDefault = 0
	fgGray    = 0
	fgGreen   = 0
	fgCyan    = 0
	fgRed     = 0
	fgYellow  = 0
)

func printBanner(url, dir, dbPath string, newDB bool) {
	bar := strings.Repeat("=", 58)
	fmt.Println()
	fmt.Println("  " + bar)
	fmt.Println("   " + appTitle + "   " + buildInfo())
	fmt.Println("  " + bar)
	fmt.Println()
	fmt.Println("   程序已在后台运行，界面用浏览器访问：")
	fmt.Println()
	fmt.Println("       " + url)
	fmt.Println()
	if newDB {
		fmt.Println("   数据库：" + dbPath + "  （首次运行，已新建空库）")
	} else {
		fmt.Println("   数据库：" + dbPath)
	}
	fmt.Println("   程序目录：" + dir)
	fmt.Println()
	fmt.Println("   提示：浏览器没自动打开时，复制上面的地址粘贴到浏览器即可")
	fmt.Println("   退出：按 Ctrl+C 正常退出")
	fmt.Println()
}

func printExitHint() {
	fmt.Println()
	fmt.Println("   服务已停止，本窗口可以关闭了。")
}

func waitClose(seconds int, hint string) {
	if hint != "" {
		fmt.Println("   " + hint)
	}
	time.Sleep(time.Duration(seconds) * time.Second)
}

func holdOnExit(seconds int) {
	waitClose(seconds, fmt.Sprintf("窗口将在 %d 秒后自动关闭。", seconds))
}

func openBrowserImpl(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func showErrorBox(title, msg string) {
	fmt.Printf("[%s] %s\n", title, msg)
	_ = os.Stderr
}
