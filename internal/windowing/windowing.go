// Package windowing 收拢独立窗口的生命周期策略：什么时候建、第一次显示落在哪里、关闭请求
// 谁来处理、空闲之后什么时候销毁。
//
// 宿主（Wails）只以 Host / Window 两个接口出现——适配器是唯一知道宿主类型的地方，策略本身
// 不碰任何宿主对象。原先这段策略散在三个创建方法里、各带一个定时器字段与两处 `switch name`，
// 而且在无头环境（`getApp()` 为 nil）下没有任何验证路径：现在它可以用假窗口与假时钟直接测。
//
// 策略回调（Policy 里的那几个）不在注册表的锁内执行：它们正是「拦下关闭之后做点别的」这种
// 需要在回调里再操作窗口的地方，锁内调用会立刻自锁。
package windowing

import (
	"sync"
	"time"
)

// Colour 是窗口底色，宿主中立。
type Colour struct {
	R, G, B, A uint8
}

// Rect 是屏幕工作区。
type Rect struct {
	X, Y, Width, Height int
}

// Options 是一个窗口的声明。它只用桌面端自己的词汇描述窗口，宿主适配器负责翻译。
type Options struct {
	Name          string
	Title         string
	URL           string
	Width         int
	Height        int
	MinWidth      int // 0 表示不限制
	MinHeight     int // 0 表示不限制
	MaxWidth      int // 0 表示不限制
	MaxHeight     int // 0 表示不限制
	Frameless     bool
	Transparent   bool
	DisableResize bool
	Hidden        bool
	Background    Colour

	// X / Y / Positioned 是创建时的初始位置：Positioned 为 true 时用 X/Y，否则由宿主按
	// 自己的缺省（居中）处理。注册表按 Policy.Position 填这三项。
	X, Y       int
	Positioned bool
}

// Direction 是关闭请求的处理结论。
type Direction int

const (
	// AllowClose 交给宿主销毁窗口。
	AllowClose Direction = iota
	// CancelClose 拦下这次关闭，窗口留下（收尾动作由策略自己完成）。
	CancelClose
)

// Window 是策略用得到的窗口能力。
type Window interface {
	Show()
	Hide()
	Raise()
	Minimise()
	Close()
	IsVisible() bool
	SetSize(width, height int)
	SetPosition(x, y int)
	SetAlwaysOnTop(on bool)
	SetBackground(colour Colour)
}

// Host 打开窗口：同名窗口已存在时返回它，否则按声明新建，并把关闭请求接到 onClose 上。
//
// onClose 必须在窗口真正关闭之前被调用；返回 true 表示这次关闭要拦下，宿主据此取消关闭。
// 闲置销毁自己发起的关闭放行。
type Host interface {
	Open(options Options, onClose func() bool) (Window, bool)
	Find(name string) (Window, bool)
	WorkArea() (Rect, bool)
}

// Clock 提供「多久之后」。测试用假时钟，生产用 time.AfterFunc。
type Clock interface {
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer 是一次待触发的定时器。
type Timer interface {
	Stop() bool
}

// Policy 是一个窗口自己的生命周期策略。
type Policy struct {
	// OnClose 处理一次关闭请求。返回 CancelClose 时窗口留下，收尾动作（隐藏、通知界面、
	// 安排闲置销毁）由它自己完成。
	OnClose func(r *Registry) Direction
	// Busy 报告此刻这件事能不能被闲置销毁打断（例如文件信息窗口的队列里还有等待确认的项）。
	// 为空表示随时可以销毁。
	Busy func() bool
	// IdleDestroy 报告这个窗口此刻是否参与「闲置即销毁」（应用策略里通常是轻量模式开关）。
	// 为空表示不参与。
	IdleDestroy func() bool
	// OnIdleDestroy 在真正销毁之前调整窗口自己的状态（例如进度窗口下次要重新定位）。
	OnIdleDestroy func(r *Registry)
	// Position 决定第一次显示落在哪里；为空表示交给宿主（缺省居中）。
	Position func(work Rect) (x, y int, ok bool)
}

// Spec 是一个窗口的完整声明。
type Spec struct {
	Options Options
	Policy  Policy
}

// state 是一个窗口的运行时状态。声明只存在 specs 里一份，这里不另存副本——否则
// 「再次 Declare 覆盖声明」就会对已经建过状态的窗口失效。
type state struct {
	timer      Timer
	positioned bool
	destroying bool
}

// Registry 持有全部窗口的声明与策略。
type Registry struct {
	host  Host
	clock Clock
	grace time.Duration

	mu     sync.Mutex
	specs  map[string]Spec
	states map[string]*state
}

// New 组装注册表：宿主适配器、时钟与闲置销毁的宽限期。
func New(host Host, clock Clock, grace time.Duration) *Registry {
	return &Registry{
		host:   host,
		clock:  clock,
		grace:  grace,
		specs:  make(map[string]Spec),
		states: make(map[string]*state),
	}
}

// Declare 登记窗口声明。同名重复登记会覆盖。
func (r *Registry) Declare(specs ...Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, spec := range specs {
		r.specs[spec.Options.Name] = spec
	}
}

// state 取（必要时建）一个窗口的运行时状态；调用方必须持有锁。
func (r *Registry) state(name string) *state {
	if st, ok := r.states[name]; ok {
		return st
	}
	st := &state{}
	r.states[name] = st
	return st
}

// spec 取一个窗口的声明；没有登记过的窗口得到零值声明。
func (r *Registry) spec(name string) Spec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.specs[name]
}

// Ensure 保证窗口存在（按声明创建），但不显示它。hidden 只影响创建那一刻：静默启动时
// 主窗口先建出来、不显示。openURL 同样只在创建时生效——窗口的地址在创建那一刻定下
// （例如进度窗口带初始聚焦的任务），此后每次显示走事件而不是改地址。
func (r *Registry) Ensure(name, openURL string, hidden bool) (Window, bool) {
	if win, ok := r.host.Find(name); ok {
		return win, true
	}
	spec := r.spec(name)
	options := spec.Options
	options.Hidden = hidden
	if openURL != "" {
		options.URL = openURL
	}
	if spec.Policy.Position != nil {
		if work, ok := r.host.WorkArea(); ok {
			if x, y, ok := spec.Policy.Position(work); ok {
				options.X, options.Y, options.Positioned = x, y, true
			}
		}
	}
	win, ok := r.host.Open(options, func() bool { return r.onCloseRequested(name) })
	if !ok {
		return nil, false
	}
	r.markPositioned(name, options.Positioned)
	return win, true
}

// Show 显示（必要时先创建）并置前。
func (r *Registry) Show(name, openURL string) (Window, bool) {
	r.CancelIdleDestroy(name)

	win, ok := r.Ensure(name, openURL, false)
	if !ok {
		return nil, false
	}
	r.applyPosition(name, win)
	win.Show()
	return win, true
}

// Find 返回已经存在的窗口。
func (r *Registry) Find(name string) (Window, bool) {
	return r.host.Find(name)
}

// Hide 隐藏窗口，并按策略安排闲置销毁。
func (r *Registry) Hide(name string) {
	win, ok := r.host.Find(name)
	if !ok {
		return
	}
	win.Hide()
	r.ScheduleIdleDestroy(name)
}

// ScheduleIdleDestroy 单独安排一次闲置销毁。窗口自己做完收尾（例如先通知界面）之后再调用它，
// 用于隐藏与安排之间还有别的事要做的路径。
func (r *Registry) ScheduleIdleDestroy(name string) {
	policy := r.spec(name).Policy
	if r.grace <= 0 || policy.IdleDestroy == nil || !policy.IdleDestroy() {
		return
	}

	r.mu.Lock()
	st := r.state(name)
	r.cancelIdleLocked(st)
	st.timer = r.clock.AfterFunc(r.grace, func() { r.destroyIfIdle(name, st) })
	r.mu.Unlock()
}

// CancelIdleDestroy 取消尚未触发的闲置销毁。
func (r *Registry) CancelIdleDestroy(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelIdleLocked(r.state(name))
}

// SetBackground 换掉窗口底色（主题切换时同步）。
func (r *Registry) SetBackground(name string, colour Colour) {
	if win, ok := r.host.Find(name); ok {
		win.SetBackground(colour)
	}
}

// CloseHidden 立刻销毁一个不可见的窗口，不必等宽限期：轻量模式打开时用它收掉热备的
// 渲染进程。可见的窗口不动。
func (r *Registry) CloseHidden(name string) {
	win, ok := r.host.Find(name)
	if !ok || win.IsVisible() {
		return
	}
	r.destroy(win, name)
}

// ResetPosition 让这个窗口下次显示时重新定位。
func (r *Registry) ResetPosition(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state(name).positioned = false
}

// Shutdown 取消全部闲置销毁，供退出时收尾。
func (r *Registry) Shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range r.states {
		r.cancelIdleLocked(st)
	}
}

func (r *Registry) markPositioned(name string, positioned bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state(name).positioned = positioned
}

func (r *Registry) applyPosition(name string, win Window) {
	r.mu.Lock()
	st := r.state(name)
	position := r.specs[name].Policy.Position
	if st.positioned || position == nil {
		r.mu.Unlock()
		return
	}
	st.positioned = true
	r.mu.Unlock()

	work, ok := r.host.WorkArea()
	if !ok {
		return
	}
	if x, y, ok := position(work); ok {
		win.SetPosition(x, y)
	}
}

// onCloseRequested 返回 true 表示拦下这次关闭。
func (r *Registry) onCloseRequested(name string) bool {
	r.mu.Lock()
	st := r.state(name)
	onClose := r.specs[name].Policy.OnClose
	if st.destroying || onClose == nil {
		r.mu.Unlock()
		return false
	}
	r.mu.Unlock()

	return onClose(r) == CancelClose
}

func (r *Registry) cancelIdleLocked(st *state) {
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
}

// destroyIfIdle 是宽限期到点时的收尾：期间被重新唤起、队列里还有等待确认的项，或者窗口
// 又被显示出来了，都不销毁。
func (r *Registry) destroyIfIdle(name string, st *state) {
	r.mu.Lock()
	if st.timer == nil {
		r.mu.Unlock()
		return
	}
	st.timer = nil
	busy := r.specs[name].Policy.Busy
	r.mu.Unlock()

	if busy != nil && busy() {
		return
	}
	win, ok := r.host.Find(name)
	if !ok || win.IsVisible() {
		return
	}
	r.destroy(win, name)
}

// destroy 销毁一个窗口：置上「这次关闭是我们自己发起的」标记，免得关闭钩子把它拦回来；
// 窗口自己的收尾状态（例如进度窗口下次重新定位）由 OnIdleDestroy 完成。
func (r *Registry) destroy(win Window, name string) {
	if onIdleDestroy := r.spec(name).Policy.OnIdleDestroy; onIdleDestroy != nil {
		onIdleDestroy(r)
	}

	r.mu.Lock()
	st := r.state(name)
	st.destroying = true
	r.mu.Unlock()
	win.Close()
	r.mu.Lock()
	st.destroying = false
	r.mu.Unlock()
}
