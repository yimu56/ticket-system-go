package main

import "runtime"

// Version 语义化版本号，发布时通过 -ldflags 覆盖：
//
//	go build -ldflags "-X main.Version=1.1.0 -s -w"
var Version = "1.0.0"

// 构建元信息，同样由 -ldflags 注入（缺省值保证 go run 时可用）
var (
	Commit    = "dev"     // 提交短哈希
	BuildTime = "unknown" // 构建时间
)

// buildInfo 生成版本信息行
func buildInfo() string {
	return "v" + Version +
		" (" + runtime.GOOS + "/" + runtime.GOARCH + ", commit " + Commit + ", built " + BuildTime + ")"
}
