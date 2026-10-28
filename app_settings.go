// 设置应用路径在桌面端的落点：把各运行子系统接成 internal/settingsapply 的依赖。
//
// 这个文件只做翻译——哪个字段是谁的、没有这个子系统时怎么装作没有；「哪次变更推给谁、
// 按什么顺序、端口迁移失败在哪里回滚」都在那个包里，启动、保存设置与重启端口三条入口
// 因此共用同一份实现。
package main

import (
	"fmt"

	"sheep-get/internal/chrome"
	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/protocol"
	"sheep-get/internal/server"
	"sheep-get/internal/settingsapply"
	"sheep-get/internal/skill"
)

// wireSettingsApply 接上设置应用路径，并把当前快照推给各子系统：启动路径就是这一次推送。
func (a *App) wireSettingsApply() error {
	applier, err := settingsapply.New(a.settings, a.settingsSubsystems())
	if err != nil {
		return err
	}
	a.settingsApply = applier
	return nil
}

// settingsSubsystems 列出这次运行里实际存在的子系统。
func (a *App) settingsSubsystems() settingsapply.Deps {
	return settingsapply.Deps{
		Engine:    engineSettings{manager: a.manager},
		Server:    loopbackSettings{app: a},
		Logging:   loggingSettings{app: a},
		Clipboard: clipboardSettings{app: a},
		Autostart: autostartSettings{app: a},
		Updater:   updaterSettings{app: a},
		Theme:     themeSettings{app: a},
		Windows:   windowSettings{app: a},
		Events:    eventSettings{app: a},
		Skill:     skillSettings{app: a},
	}
}

// engineSettings 把设置推到传输引擎的运行期配置上。
type engineSettings struct {
	manager *engine.Manager
}

func (s engineSettings) SetMaxActiveTasks(max int) {
	if s.manager != nil {
		s.manager.SetMaxActiveTasks(max)
	}
}

func (s engineSettings) SetTempDirectory(dir string) {
	if s.manager != nil {
		s.manager.SetTempDirectory(dir)
	}
}

func (s engineSettings) SetUseServerFileTime(enabled bool) {
	if s.manager != nil {
		s.manager.SetUseServerFileTime(enabled)
	}
}

func (s engineSettings) SetProxy(mode string, addrs ...string) error {
	if s.manager == nil {
		return nil
	}
	return s.manager.SetProxy(mode, addrs...)
}

// loopbackSettings 把设置推到本地环回通道。
type loopbackSettings struct {
	app *App
}

func (s loopbackSettings) SetTargetPort(port int) {
	if srv := s.app.loopbackServer; srv != nil {
		srv.SetTargetPort(port)
	}
}

func (s loopbackSettings) SetAPIKey(key string) {
	if srv := s.app.loopbackServer; srv != nil {
		srv.SetAPIKey(key)
	}
}

func (s loopbackSettings) BroadcastTakeover(sync server.TakeoverConfigSync) {
	if srv := s.app.loopbackServer; srv != nil {
		srv.BroadcastTakeoverConfig(sync)
	}
}

func (s loopbackSettings) Restart(port int) error {
	if s.app.loopbackServer == nil {
		return fmt.Errorf("loopback server not initialized")
	}
	return s.app.loopbackServer.Restart(port)
}

func (s loopbackSettings) Port() int {
	if srv := s.app.loopbackServer; srv != nil {
		return srv.Port()
	}
	return 0
}

type loggingSettings struct {
	app *App
}

func (s loggingSettings) Apply(enabled bool) {
	s.app.applyLogging(enabled)
}

type clipboardSettings struct {
	app *App
}

func (s clipboardSettings) SetEnabled(enabled bool) {
	if watcher := s.app.clipboardWatcher; watcher != nil {
		watcher.SetEnabled(enabled)
	}
}

// autostartSettings 把自启动开关同步到系统。启动早期宿主还没就绪，这时只清理系统里
// StartupApproved 的残留记录；真正的开关写入发生在用户改动设置之后。
type autostartSettings struct {
	app *App
}

func (s autostartSettings) SetEnabled(enabled bool) {
	s.app.syncLaunchAtStartup(enabled)
}

type updaterSettings struct {
	app *App
}

func (s updaterSettings) SetProxy(mode string, addrs ...string) {
	if s.app.updater == nil {
		return
	}
	// 更新检查有自己的一条网络出口；它配不上代理时下载与设置本身照常有效，
	// 因此只记一条日志，不把整次设置变更判为失败。
	if err := s.app.updater.SetProxy(mode, addrs...); err != nil {
		s.app.log().Warn("更新服务代理设置失败", "error", err.Error())
	}
}

type themeSettings struct {
	app *App
}

func (s themeSettings) SetMode(mode string) {
	wailsApp := s.app.getApp()
	if wailsApp == nil {
		return
	}
	for _, w := range wailsApp.Window.GetAll() {
		chrome.SetNativeWindowTheme(w, mode)
	}
}

type windowSettings struct {
	app *App
}

func (s windowSettings) SetBackground(light bool) {
	if s.app.windows == nil {
		return
	}
	s.app.windows.SetBackground(winNameMain, backgroundColour(light))
}

func (s windowSettings) CloseHidden() {
	if s.app.windows == nil {
		return
	}
	s.app.windows.CloseHidden(winNameMain)
	s.app.windows.CloseHidden(winNameProgress)
}

type eventSettings struct {
	app *App
}

func (s eventSettings) PublishSettingsUpdated(st *config.Settings) {
	s.app.emit(protocol.EventSettingsUpdated, st)
}

// skillSettings 渲染 Agent Skill 文件。写不出来只影响那份说明文件，不影响下载与设置。
type skillSettings struct {
	app *App
}

func (s skillSettings) Sync(port int, apiKey string) {
	if s.app.storage == nil {
		return
	}
	if err := skill.Sync(s.app.storage.ExecDir, port, apiKey); err != nil {
		s.app.log().Warn("同步 Agent Skill 文件失败", "error", err.Error(), "execDir", s.app.storage.ExecDir)
	}
}
