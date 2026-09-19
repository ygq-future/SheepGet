package main

// 窗口名与窗口几何的单一命名来源。main.go 预创建窗口、app.go 动态操作窗口时
// 共用这些常量，避免同一标识散落成多处字符串/数字字面量。

// 窗口名。
const (
	winNameMain     = "main"
	winNameFileInfo = "fileinfo"
	winNameProgress = "progress"
)

// 进度窗口几何。main.go 预创建与 app.go 动态重建/调整时共用，保证两处尺寸一致。
const (
	progressWindowWidth    = 560
	progressWindowHeight   = 160
	progressWindowMinWidth = 560
	progressWindowMaxWidth = 560
	// progressWindowMinH 不是内容下限，而是窗口自身的防御值：窗口高度由内容决定，
	// 空面板根本不会显示，任何一个真实内容高度都远高于它，这里只用于挡住非法值
	// （内容还没测量出来）以及用户手动把窗口拖到不可用的尺寸。
	progressWindowMinH = 96
	progressWindowMaxH = 640
	// progressWindowEdgeGap 是进度窗口停靠屏幕右下角时离边缘的间距。
	progressWindowEdgeGap = 32
	// progressWindowBottomOffset 是进度窗口停靠时底部预留的高度，让窗口从默认高度
	// 向上展开（160 → 640）时有足够的空间，不至于顶出屏幕。
	progressWindowBottomOffset = 320
)

// 文件信息窗口几何。
const (
	fileInfoWindowWidth = 460
	fileInfoWindowMinH  = 240
	fileInfoWindowMaxH  = 700
)
