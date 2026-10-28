package chrome

// WindowsAdditionalBrowserArgs 是 Windows 平台 WebView2 启动加速参数（裁剪非桌面必要服务，缩短内核冷启动耗时）。
var WindowsAdditionalBrowserArgs = []string{
	"--disable-features=RendererCodeIntegrity,msEdgeTranslate,Translate,OptimizationHints,MediaRouter",
	"--disable-component-update",
	"--disable-extensions",
	"--disable-default-apps",
	"--no-default-browser-check",
	"--disable-sync",
}
