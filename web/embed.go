// 前端构建产物的嵌入点。dist 由 `npm run build` 生成（仓库中带有一份
// 占位 index.html，保证未构建前端时 Go 侧仍可编译）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist 返回前端静态资源文件系统。
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
