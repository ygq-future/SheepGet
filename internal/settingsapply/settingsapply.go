// Package settingsapply 是设置快照到达运行子系统的唯一路径。
//
// 谁接收设置里的哪一段、按什么顺序、端口迁移失败在哪里回滚，都只在这里定义一次。启动时
// 推送当前快照（New）、保存设置（Update）与重启端口（RestartPort）三条入口共用它，调用方
// 不再各自编排顺序。子系统按上下文拆开注入，测试用假实现断言某次变更实际到达了谁。
//
// 一次变更只推真的变了的那几段：每条推送都对应一个可观察的副作用（广播给扩展、改写 Skill
// 文件、销毁热备窗口），没有变化就不该发生。每条推送都必须幂等——失败后重试会再推一遍。
package settingsapply

import (
	"fmt"
	"slices"
	"sync"

	"sheep-get/internal/config"
	"sheep-get/internal/protocol"
	"sheep-get/internal/server"
)

// Store 是设置快照的持久化边界；应用路径是它唯一的写入者。
type Store interface {
	Get() config.Settings
	Update(config.Settings) (config.Settings, error)
}

// Engine 是传输引擎的运行期配置面。
type Engine interface {
	SetMaxActiveTasks(max int)
	SetTempDirectory(dir string)
	SetUseServerFileTime(enabled bool)
	SetProxy(mode string, addrs ...string) error
}

// Server 是本地环回通道。
type Server interface {
	SetTargetPort(port int)
	SetAPIKey(key string)
	BroadcastTakeover(sync server.TakeoverConfigSync)
	Restart(port int) error
	Port() int
}

// Logging 是运行日志出口。
type Logging interface {
	Apply(enabled bool)
}

// Clipboard 是剪贴板监视。
type Clipboard interface {
	SetEnabled(enabled bool)
}

// Autostart 是系统自启动项。
type Autostart interface {
	SetEnabled(enabled bool)
}

// Updater 是更新服务的网络出口。
type Updater interface {
	SetProxy(mode string, addrs ...string)
}

// Theme 是原生窗口的深浅外观。
type Theme interface {
	SetMode(mode string)
}

// Windows 是已创建窗口的底色与轻量模式下的热备回收。
type Windows interface {
	SetBackground(light bool)
	CloseHidden()
}

// Events 把整份快照推给界面。
type Events interface {
	PublishSettingsUpdated(s *config.Settings)
}

// Skill 按实际可达的端口与密钥重写 Agent Skill 文件。
type Skill interface {
	Sync(port int, apiKey string)
}

// Deps 是一次运行里的全部设置落点。缺哪个就按「这次没有它」处理：无头环境与启动早期
// 本来就没有窗口、剪贴板监视这些子系统。
type Deps struct {
	Engine    Engine
	Server    Server
	Logging   Logging
	Clipboard Clipboard
	Autostart Autostart
	Updater   Updater
	Theme     Theme
	Windows   Windows
	Events    Events
	Skill     Skill
}

// Applier 持有「设置 → 子系统」的映射与顺序，以及端口迁移的回滚。
type Applier struct {
	store Store
	deps  Deps

	// mu 让一次变更的推送与端口迁移互斥：设置变更来自界面、环回接口与启动流程，
	// 叠在一起时不能让两次推送交错。
	mu sync.Mutex
	// applied 是上一次真正推给子系统的快照，用来判断这次变更要动谁；primed 为假时
	// 还没有基准，New 的第一次推送因此是全量。
	applied config.Settings
	primed  bool
}

// New 接上应用路径，并立即把存下来的快照推给各子系统——启动路径就是这一次推送。
// 返回的错误只有一种来源：代理被传输引擎拒绝。此时运行状态保持默认，不能带着一个
// 不生效的代理继续跑。
func New(store Store, deps Deps) (*Applier, error) {
	a := &Applier{store: store, deps: deps}
	a.mu.Lock()
	defer a.mu.Unlock()

	snapshot := store.Get()
	if err := a.applyLocked(&snapshot); err != nil {
		return nil, err
	}
	return a, nil
}

// Update 持久化一份设置，并把这次变更推给相应子系统。端口变了就先迁移服务：绑定失败时
// 磁盘与运行状态都还没动，错误直接交回界面，用户看到的就是「设置没保存」。
func (a *Applier) Update(req config.Settings) (config.Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.store.Get()
	next := req
	if target := next.General.ServerPort; target > 0 && target != prev.General.ServerPort {
		if err := a.deps.Server.Restart(target); err != nil {
			return prev, fmt.Errorf("本地端口切换失败: %w", err)
		}
		next.General.ServerPort = a.deps.Server.Port()
	}
	return a.commit(prev, next, next.General.ServerPort != prev.General.ServerPort)
}

// RestartPort 在 port 上重新绑定环回服务，并把实际绑定的端口记进设置；port <= 0 时用
// 设置里的端口。目标端口被占用而顺延时，记的是真正绑上的那个——界面与 Skill 文件因此
// 都指向还能连上的端口。
func (a *Applier) RestartPort(port int) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.store.Get()
	target := port
	if target <= 0 {
		target = prev.General.ServerPort
	}
	if target <= 0 {
		target = protocol.PortDefaultServer
	}
	if err := a.deps.Server.Restart(target); err != nil {
		return 0, err
	}

	bound := a.deps.Server.Port()
	if bound == prev.General.ServerPort {
		return bound, nil
	}

	next := prev
	next.General.ServerPort = bound
	if _, err := a.commit(prev, next, true); err != nil {
		return 0, err
	}
	return bound, nil
}

// AdoptPort 记录启动时真正绑定的端口：设置里的端口被占用而顺延时，设置、界面与 Skill
// 文件都必须指向实际可连的那一个。
func (a *Applier) AdoptPort(port int) error {
	if port <= 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.store.Get()
	if prev.General.ServerPort == port {
		return nil
	}
	next := prev
	next.General.ServerPort = port
	// 端口是启动时就已经绑好的事实，写不进去只需要报错，没有可回滚的动作。
	_, err := a.commit(prev, next, false)
	return err
}

// commit 落盘一份新快照并推给子系统。undoPort 表示这次端口迁移是本模块做的：写不进磁盘
// 时把它迁回旧端口——整条设置路径上唯一的回滚动作。
func (a *Applier) commit(prev, next config.Settings, undoPort bool) (config.Settings, error) {
	saved, err := a.store.Update(next)
	if err != nil {
		if undoPort {
			if rollbackErr := a.deps.Server.Restart(prev.General.ServerPort); rollbackErr != nil {
				return saved, fmt.Errorf("%w（端口回滚失败: %v）", err, rollbackErr)
			}
		}
		return saved, err
	}
	if err := a.applyLocked(&saved); err != nil {
		return saved, err
	}
	return saved, nil
}

// SyncSkill 按当前快照与运行端口重写 Agent Skill 文件，供打开目录前补齐。
func (a *Applier) SyncSkill() {
	a.mu.Lock()
	defer a.mu.Unlock()

	current := a.store.Get()
	a.deps.Skill.Sync(a.skillPort(current), current.General.APIKey)
}

// applyLocked 按固定顺序把一份快照推给子系统。调用方持有 mu。
func (a *Applier) applyLocked(next *config.Settings) error {
	if next == nil {
		return nil
	}
	prev := a.applied
	// first 表示还没有基准（启动那一次）：此时每段都要推，否则初始状态不会到达子系统。
	first := !a.primed

	if first || prev.Download.MaxConcurrentDownloads != next.Download.MaxConcurrentDownloads {
		a.deps.Engine.SetMaxActiveTasks(next.Download.MaxConcurrentDownloads)
	}
	if first || prev.Download.TempDirectory != next.Download.TempDirectory {
		a.deps.Engine.SetTempDirectory(next.Download.TempDirectory)
	}
	if first || prev.Download.UseServerFileTime != next.Download.UseServerFileTime {
		a.deps.Engine.SetUseServerFileTime(next.Download.UseServerFileTime)
	}
	if first || !sameProxy(prev.Proxy, next.Proxy) {
		addrs := next.Proxy.EffectiveCustomAddrs()
		// 代理被拒绝是用户立刻能感知的失败：后面的子系统不再动，变更整体交回调用方。
		if err := a.deps.Engine.SetProxy(string(next.Proxy.Mode), addrs...); err != nil {
			return err
		}
		a.deps.Updater.SetProxy(string(next.Proxy.Mode), addrs...)
	}
	if first || prev.General.EnableLogging != next.General.EnableLogging {
		a.deps.Logging.Apply(next.General.EnableLogging)
	}
	if first || !sameTakeover(&prev, next) {
		a.deps.Server.BroadcastTakeover(TakeoverSync(next))
	}
	if first || prev.General.APIKey != next.General.APIKey {
		a.deps.Server.SetAPIKey(next.General.APIKey)
	}
	if first || prev.General.ServerPort != next.General.ServerPort {
		a.deps.Server.SetTargetPort(next.General.ServerPort)
	}
	if first || prev.Clipboard.Enabled != next.Clipboard.Enabled {
		a.deps.Clipboard.SetEnabled(next.Clipboard.Enabled)
	}
	if first || prev.General.LaunchAtStartup != next.General.LaunchAtStartup {
		a.deps.Autostart.SetEnabled(next.General.LaunchAtStartup)
	}
	if first || prev.Appearance.Theme != next.Appearance.Theme {
		a.deps.Theme.SetMode(string(next.Appearance.Theme))
		a.deps.Windows.SetBackground(next.Appearance.Theme == config.ThemeLight)
	}
	// 轻量模式只在刚打开那一刻收热备窗口：之后改别的设置不该顺手销毁窗口。
	if next.General.LightweightMode && (first || !prev.General.LightweightMode) {
		a.deps.Windows.CloseHidden()
	}
	// 界面每次都拿整份快照：它要据此重绘，不看这次改了哪几段。
	a.deps.Events.PublishSettingsUpdated(next)
	if first || prev.General.ServerPort != next.General.ServerPort || prev.General.APIKey != next.General.APIKey {
		a.deps.Skill.Sync(a.skillPort(*next), next.General.APIKey)
	}

	a.applied = *next
	a.primed = true
	return nil
}

// skillPort 给出扩展与 Agent 眼下真正能连上的端口：服务已经绑定时以实际端口为准，
// 还没启动时用设置里记的那个。
func (a *Applier) skillPort(next config.Settings) int {
	if port := a.deps.Server.Port(); port > 0 {
		return port
	}
	if next.General.ServerPort > 0 {
		return next.General.ServerPort
	}
	return protocol.PortDefaultServer
}

// TakeoverSync 把设置翻译成扩展要的接管规则。配置接口与推送共用这一份，扩展不会因为
// 读还是推而拿到两套口径。
func TakeoverSync(st *config.Settings) server.TakeoverConfigSync {
	if st == nil {
		return server.TakeoverConfigSync{}
	}
	return server.TakeoverConfigSync{
		Extensions:    st.Download.AllExtensions(),
		ExcludedSites: st.Takeover.ExcludedSites,
		PauseShortcut: st.Takeover.PauseShortcut,
		ForceShortcut: st.Takeover.ForceShortcut,
	}
}

func sameProxy(a, b config.ProxyConfig) bool {
	return a.Mode == b.Mode && slices.Equal(a.EffectiveCustomAddrs(), b.EffectiveCustomAddrs())
}

// sameTakeover 判断这次变更是否动到了扩展眼里的接管规则：后缀全集、排除站点与快捷键。
func sameTakeover(a, b *config.Settings) bool {
	return slices.Equal(a.Download.AllExtensions(), b.Download.AllExtensions()) &&
		slices.Equal(a.Takeover.ExcludedSites, b.Takeover.ExcludedSites) &&
		a.Takeover.PauseShortcut == b.Takeover.PauseShortcut &&
		a.Takeover.ForceShortcut == b.Takeover.ForceShortcut
}
