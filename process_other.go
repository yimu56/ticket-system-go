//go:build !windows

package main

import (
	"os"
	"strconv"
	"syscall"
)

// processAlive 判断指定 PID 是否仍在运行（发送 0 号信号探测）
func processAlive(pidStr string) bool {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
