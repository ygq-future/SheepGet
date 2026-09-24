package main

import (
	"time"

	"sheep-get/internal/windowing"
)

// 窗口名与窗口几何的单一命名来源：窗口的三份声明在 app_windows.go，这里只留标识与尺寸这一层，
// 避免同一标识散落成多处字符串/数字字面量。

// 窗口名。
const (
	winNameMain     = "main"
	winNameFileInfo = "fileinfo"
	winNameProgress = "progress"
)

// 主窗口几何。
const (
	mainWindowWidth  = 800
	mainWindowHeight = 520
)

// 主窗口默认背景色（与前端暗色/亮色基调 #09090b / #f8fafc 完全统一，避免深蓝模板色与黑白主题割裂）。
var (
	mainWindowDarkBackgroundColour  = windowing.Colour{R: 9, G: 9, B: 11, A: 255}
	mainWindowLightBackgroundColour = windowing.Colour{R: 248, G: 250, B: 252, A: 255}
)

// Windows 平台 WebView2 启动加速参数（裁剪非桌面必要服务，缩短内核冷启动耗时）。
var windowsAdditionalBrowserArgs = []string{
	"--disable-features=RendererCodeIntegrity,msEdgeTranslate,Translate,OptimizationHints,MediaRouter",
	"--disable-component-update",
	"--disable-extensions",
	"--disable-default-apps",
	"--no-default-browser-check",
	"--disable-sync",
}

// 进度窗口几何。创建声明与内容驱动的高度调整共用同一组数字。
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
	fileInfoWindowWidth  = 460
	fileInfoWindowHeight = 300
	fileInfoWindowMinH   = 240
	fileInfoWindowMaxH   = 700
)

// 独立浮动窗口（新建下载与进度窗口）在轻量模式下的闲置销毁宽限期。
// 连续操作期间保持热备秒开；空闲超过此宽限期且无活动任务时才在后台安全销毁 WebView 渲染进程。
const windowIdleDestroyGracePeriod = 60 * time.Second
