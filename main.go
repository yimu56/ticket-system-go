// 工单管理系统 —— Go 单文件版本
// 数据层兼容原 Python 版 ticket.db，界面为内嵌 Web UI。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ticketsystem/internal/server"
	"ticketsystem/internal/store"
	"ticketsystem/internal/webui"
)

const appTitle = "工单管理系统"

// baseDir 可执行文件所在目录；数据库与日志都放在这里，保证绿色单文件
func baseDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func main() {
	var (
		port    = flag.Int("port", 0, "HTTP 端口，0 表示自动选择空闲端口")
		dbPath  = flag.String("db", "", "数据库文件路径，默认为程序目录下的 ticket.db")
		noOpen  = flag.Bool("no-open", false, "启动后不自动打开浏览器")
		devMode = flag.Bool("dev", false, "开发模式：打印访问地址并禁用日志重定向")
		showVer = flag.Bool("v", false, "显示版本")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s %s\n", appTitle, buildInfo())
		return
	}

	dir := baseDir()
	if *dbPath == "" {
		*dbPath = filepath.Join(dir, "ticket.db")
	}

	// 日志：Windows 无控制台时 stderr 不可写，重定向到文件
	logFile := setupLog(dir, *devMode)
	if logFile != nil {
		defer logFile.Close()
	}

	log.Printf("=== %s %s 启动 ===", appTitle, buildInfo())
	log.Printf("程序目录: %s", dir)
	log.Printf("数据库: %s", *dbPath)

	st, err := store.Open(*dbPath)
	if err != nil {
		fatal("打开数据库失败: " + err.Error())
	}
	defer st.Close()

	srv := server.New(st, webui.FS(), "v"+Version)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("监听端口失败: " + err.Error())
	}
	realAddr := ln.Addr().String()
	url := "http://" + realAddr + "/"
	log.Printf("服务已启动: %s", url)

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP 服务异常: %v", err)
		}
	}()

	if !*noOpen {
		if err := openBrowser(url); err != nil {
			log.Printf("自动打开浏览器失败: %v，请手动访问 %s", err, url)
			fmt.Println("请手动在浏览器打开:", url)
		}
	} else {
		fmt.Println("服务地址:", url)
	}
	if *devMode {
		fmt.Println("服务地址:", url)
	}

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("正在关闭…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("关闭服务出错: %v", err)
	}
	log.Println("已退出")
}

// fatal 记录并弹窗提示（GUI 模式下用户看不到控制台）
func fatal(msg string) {
	log.Println("致命错误:", msg)
	showErrorBox(appTitle, msg)
	os.Exit(1)
}

// setupLog 配置日志输出；devMode 下只输出到控制台
func setupLog(dir string, dev bool) *os.File {
	if dev {
		log.SetFlags(log.LstdFlags | log.Lmsgprefix)
		return nil
	}
	path := filepath.Join(dir, "ticket_system.log")
	// 超过 5MB 时轮转，最多保留一份备份
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5*1024*1024 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		// 退回临时目录，避免因只读目录直接崩溃
		f, err = os.CreateTemp("", "ticket_system_*.log")
		if err != nil {
			return nil
		}
	}
	mw := &multiWriter{w: f}
	if !isConsoleAvailable() {
		log.SetOutput(mw)
	} else {
		log.SetOutput(f)
	}
	log.SetFlags(log.LstdFlags)
	return f
}

// multiWriter 兜底 stderr：GUI 模式下没有控制台，写入失败不应导致日志丢失
type multiWriter struct{ w *os.File }

func (m *multiWriter) Write(p []byte) (int, error) { return m.w.Write(p) }

// openBrowser 用系统默认浏览器打开地址
func openBrowser(url string) error {
	// 仅允许本地回环地址，避免被利用为任意 URL 拉起
	if !strings.HasPrefix(url, "http://127.0.0.1:") && !strings.HasPrefix(url, "http://localhost:") {
		return errors.New("拒绝打开非本地地址")
	}
	return openBrowserImpl(url)
}

func init() {
	// 保证 exec 包被引用（各平台实现里会用到）
	_ = exec.Command
}
