//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func isConsoleAvailable() bool { return true }

func openBrowserImpl(url string) error {
	// 非 Windows 环境启动子进程，但 main 中缺少仓库级硬编码，需在 main 里声明
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func showErrorBox(title, msg string) {
	fmt.Printf("[%s] %s\n", title, msg)
}
