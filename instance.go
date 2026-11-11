package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// instanceFile 记录当前运行实例的端口，用于「重复双击时直接复用」。
// 放在系统临时目录，按数据库路径做哈希区分，避免多个不同目录的实例互相干扰。
func instanceFile(dbPath string) string {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		abs = dbPath
	}
	var h uint32 = 2166136261
	for _, c := range []byte(strings.ToLower(abs)) {
		h ^= uint32(c)
		h *= 16777619
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("ticket-system-%08x.port", h))
}

// findRunningInstance 检查是否已有实例在跑，有则返回其 URL
func findRunningInstance(dbPath string) (string, bool) {
	file := instanceFile(dbPath)
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	raw := strings.TrimSpace(string(data))
	// 格式：pid:port
	pidStr, port, ok := strings.Cut(raw, ":")
	if !ok || port == "" {
		_ = os.Remove(file)
		return "", false
	}
	// 记录的进程已不在（例如被任务管理器强杀），视为残留
	if !processAlive(pidStr) {
		_ = os.Remove(file)
		return "", false
	}
	url := "http://127.0.0.1:" + port + "/"
	client := &http.Client{Timeout: 800 * time.Millisecond}
	res, err := client.Get(url + "api/version")
	if err != nil {
		_ = os.Remove(file)
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_ = os.Remove(file)
		return "", false
	}
	return url, true
}

// saveInstance 写入当前进程与端口，进程退出时清理
func saveInstance(dbPath, port string) (cleanup func()) {
	file := instanceFile(dbPath)
	content := fmt.Sprintf("%d:%s", os.Getpid(), port)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return func() {}
	}
	return func() { _ = os.Remove(file) }
}

// portFromAddr 从监听地址里取出端口，例如 127.0.0.1:52341 -> 52341
func portFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}
