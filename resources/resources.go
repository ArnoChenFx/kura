// Package resources 嵌入随应用发布的图片资源。ui 包不能直接 go:embed 仓库根下的
// resources 目录（embed 不能跨出包目录），所以由这个包把文件交出去。
package resources

import _ "embed"

// IconPNG 是应用图标（resources/icon.png，1024×1024，四周透明）。标题栏里显示它。
//
//go:embed icon.png
var IconPNG []byte
