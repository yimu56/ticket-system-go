// Package webui 通过 go:embed 把前端静态文件打包进二进制。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:assets
var embedded embed.FS

// FS 返回以 assets 为根的文件系统
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}
