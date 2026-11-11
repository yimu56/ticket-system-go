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

	// 控制台：设置标题、关闭快速编辑（否则点一下窗口输出就冻结）
	initConsole(appTitle + "  " + Version)

	// 已有实例在运行时，直接把浏览器指过去，不再开第二个窗口
	if url, ok := findRunningInstance(*dbPath); ok {
		colorln(fgGreen, "")
		colorln(fgGreen, "   程序已经在运行，正在为你打开界面：")
		colorln(fgGreen, "       "+url)
		colorln(fgGray, "")
		colorln(fgGray, "   如果浏览器没有反应，请手动复制上面的地址访问。")
		colorln(fgGray, "   这个窗口 5 秒后自动关闭。")
		if *noOpen && *devMode {
			fmt.Println(url)
			return
		}
		_ = openBrowser(url)
		holdOnExit(5)
		return
	}

	// 日志：Windows 无控制台时 stderr 不可写，重定向到文件
	logFile := setupLog(dir, *devMode)
	if logFile != nil {
		defer logFile.Close()
	}

	log.Printf("=== %s %s 启动 ===", appTitle, buildInfo())
	log.Printf("程序目录: %s", dir)
	log.Printf("数据库: %s", *dbPath)

	// 数据库是否首次创建，用于在面板上提示
	_, statErr := os.Stat(*dbPath)
	newDB := os.IsNotExist(statErr)

	st, err := store.Open(*dbPath)
	if err != nil {
		fatal("打开数据库失败: " + err.Error())
	}
	defer st.Close()

	srv := server.New(st, webui.FS(), "v"+Version)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal("监听端口失败（可加 -port 指定其它端口）: " + err.Error())
	}
	realAddr := ln.Addr().String()
	url := "http://" + realAddr + "/"
	log.Printf("服务已启动: %s", url)

	// 记录端口，供重复双击时复用
	cleanupPort := saveInstance(*dbPath, portFromAddr(realAddr))
	defer cleanupPort()

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP 服务异常: %v", err)
		}
	}()

	// 控制台面板（-dev 时保持纯净输出，不打装饰）
	if !*devMode {
		printBanner(url, dir, *dbPath, newDB)
	} else {
		fmt.Println("服务地址:", url)
	}

	if !*noOpen {
		if err := openBrowser(url); err != nil {
			log.Printf("自动打开浏览器失败: %v", err)
			if !*devMode {
				colorln(fgYellow, "   自动打开浏览器失败，请手动复制上面的地址访问。")
			}
		}
	}

	// 等待退出：Ctrl+C / 关闭窗口
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("正在关闭…")
	if !*devMode {
		fmt.Println()
		colorln(fgGray, "   正在停止服务…")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("关闭服务出错: %v", err)
	}
	log.Println("已退出")
	if !*devMode {
		printExitHint()
	}
}

// fatal 记录并弹窗提示，并在控制台留出阅读时间
func fatal(msg string) {
	log.Println("致命错误:", msg)
	colorln(fgRed, "")
	colorln(fgRed, "   [启动失败] "+msg)
	colorln(fgRed, "   详细日志请查看同目录下的 ticket_system.log")
	showErrorBox(appTitle, msg)
	if isConsoleAvailable() {
		holdOnExit(30)
	}
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
