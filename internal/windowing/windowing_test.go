package windowing_test

import (
	"testing"
	"time"

	"sheep-get/internal/windowing"
)

// 窗口生命周期的策略原先散在三个创建方法里，而无头环境下拿不到宿主对象，等于没有验证路径。
// 这里用假窗口与假时钟把那段策略钉住：闲置销毁的宽限与取消、队列非空不销毁、首次定位、
// 以及各个窗口自己的关闭语义。

type fakeTimer struct {
	fn    func()
	dead  bool
	fired bool
}

func (t *fakeTimer) Stop() bool {
	if t.dead || t.fired {
		return false
	}
	t.dead = true
	return true
}

// fakeClock 把「多久之后」变成手工触发。
type fakeClock struct {
	timers []*fakeTimer
}

func (c *fakeClock) AfterFunc(_ time.Duration, f func()) windowing.Timer {
	timer := &fakeTimer{fn: f}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) fire() {
	pending := c.timers
	c.timers = nil
	for _, timer := range pending {
		if timer.dead {
			continue
		}
		timer.fired = true
		timer.fn()
	}
}

func (c *fakeClock) pending() int {
	count := 0
	for _, timer := range c.timers {
		if !timer.dead && !timer.fired {
			count++
		}
	}
	return count
}

type fakeWindow struct {
	options    windowing.Options
	visible    bool
	closed     bool
	positioned [2]int
	onClose    func() bool
	// closeAllowed 记录宿主问过钩子之后的结论：我们自己发起的销毁必须被放行。
	closeAllowed bool
}

func (w *fakeWindow) Show()     { w.visible = true }
func (w *fakeWindow) Raise()    { w.visible = true }
func (w *fakeWindow) Hide()     { w.visible = false }
func (w *fakeWindow) Minimise() { w.visible = false }
func (w *fakeWindow) Close() {
	w.closed = true
	// 宿主在真正关闭之前问一次钩子；结论要记下来，销毁是我们自己发起时必须放行。
	w.closeAllowed = !w.onClose()
}
func (w *fakeWindow) IsVisible() bool                { return w.visible }
func (w *fakeWindow) SetPosition(x, y int)           { w.positioned = [2]int{x, y} }
func (w *fakeWindow) SetAlwaysOnTop(bool)            {}
func (w *fakeWindow) SetBackground(windowing.Colour) {}
func (w *fakeWindow) SetSize(int, int)               {}

type fakeHost struct {
	windows  map[string]*fakeWindow
	workArea windowing.Rect
	hasArea  bool
}

func newFakeHost() *fakeHost {
	return &fakeHost{windows: map[string]*fakeWindow{}}
}

func (h *fakeHost) Open(options windowing.Options, onClose func() bool) (windowing.Window, bool) {
	win := &fakeWindow{options: options, onClose: onClose}
	h.windows[options.Name] = win
	return win, true
}

func (h *fakeHost) Find(name string) (windowing.Window, bool) {
	win, ok := h.windows[name]
	if !ok {
		return nil, false
	}
	return win, true
}

func (h *fakeHost) WorkArea() (windowing.Rect, bool) { return h.workArea, h.hasArea }

func dockBottomRight(width, height, gap int) func(windowing.Rect) (int, int, bool) {
	return func(work windowing.Rect) (int, int, bool) {
		return work.X + work.Width - width - gap, work.Y + work.Height - height - gap, true
	}
}

// idleWindow 造一个「闲置即销毁 + 队列空才销毁」的窗口声明，开关由调用方给。
func idleWindow(idle func() bool, busy func() bool) windowing.Spec {
	return windowing.Spec{
		Options: windowing.Options{Name: "fileinfo"},
		Policy: windowing.Policy{
			Busy:        busy,
			IdleDestroy: idle,
			OnClose:     func(*windowing.Registry) windowing.Direction { return windowing.CancelClose },
		},
	}
}

func TestRegistry_IdleDestroyWaitsForGraceAndCancels(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	idle := true
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(idleWindow(func() bool { return idle }, func() bool { return false }))

	if _, ok := registry.Show("fileinfo", ""); !ok {
		t.Fatalf("expected the window to be created and shown")
	}
	registry.Hide("fileinfo")
	if clock.pending() != 1 {
		t.Fatalf("hiding an idle-destroy window should schedule exactly one timer, got %d", clock.pending())
	}

	// 宽限期里重新唤起：这次安排要取消，不能到点销毁一个正在用的窗口。
	if _, ok := registry.Show("fileinfo", ""); !ok {
		t.Fatalf("expected the window to be shown again")
	}
	if clock.pending() != 0 {
		t.Errorf("showing the window again must cancel the pending destroy")
	}
	clock.fire()
	if host.windows["fileinfo"].closed {
		t.Errorf("a window that was shown again must not be destroyed")
	}
}

func TestRegistry_IdleDestroySkipsBusyAndVisibleWindows(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	busy := true
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(idleWindow(func() bool { return true }, func() bool { return busy }))

	if _, ok := registry.Show("fileinfo", ""); !ok {
		t.Fatalf("expected the window to be created")
	}

	// 队列里还有等待确认的项：到点也不销毁。
	registry.Hide("fileinfo")
	clock.fire()
	if host.windows["fileinfo"].closed {
		t.Fatalf("a busy window must not be destroyed")
	}

	// 队列空了，但窗口又被显示出来：同样不销毁。
	busy = false
	if _, ok := registry.Show("fileinfo", ""); !ok {
		t.Fatalf("expected the window to be shown again")
	}
	registry.ScheduleIdleDestroy("fileinfo")
	clock.fire()
	if host.windows["fileinfo"].closed {
		t.Errorf("a visible window must not be destroyed")
	}

	// 空闲且不可见：销毁一次。销毁是我们自己发起的，关闭钩子必须放行。
	registry.Hide("fileinfo")
	clock.fire()
	if !host.windows["fileinfo"].closed {
		t.Errorf("expected the idle window to be destroyed")
	}
	if !host.windows["fileinfo"].closeAllowed {
		t.Errorf("a destroy we started ourselves must get through the close policy")
	}
	if clock.pending() != 0 {
		t.Errorf("a fired timer must not stay pending")
	}
}

func TestRegistry_IdleDestroyFollowsLightweightMode(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(idleWindow(func() bool { return false }, func() bool { return false }))

	if _, ok := registry.Show("fileinfo", ""); !ok {
		t.Fatalf("expected the window to be created")
	}
	registry.Hide("fileinfo")
	if clock.pending() != 0 {
		t.Fatalf("a non-lightweight window should not schedule a destroy, got %d", clock.pending())
	}
}

func TestRegistry_IdleDestroyRunsStateResetFirst(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	spec := idleWindow(func() bool { return true }, func() bool { return false })
	spec.Policy.OnIdleDestroy = func(r *windowing.Registry) { r.ResetPosition(spec.Options.Name) }
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(spec)

	registry.Show("fileinfo", "")
	registry.Hide("fileinfo")
	clock.fire()
	if !host.windows["fileinfo"].closed {
		t.Fatalf("expected the idle window to be destroyed")
	}
}

func TestRegistry_PositionsOnlyOnFirstShow(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	host.workArea, host.hasArea = windowing.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}, true
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(windowing.Spec{
		Options: windowing.Options{Name: "progress"},
		Policy:  windowing.Policy{Position: dockBottomRight(560, 160, 32)},
	})

	registry.Show("progress", "")
	created := host.windows["progress"]
	if !created.options.Positioned || created.options.X != 1920-560-32 || created.options.Y != 1080-160-32 {
		t.Fatalf("expected the creation to carry the docked position, got %+v", created.options)
	}

	// 定位只做一次：用户之后可能把窗口拖到别处，再显示时不去干涉。
	created.positioned = [2]int{}
	registry.Show("progress", "")
	if created.positioned != [2]int{0, 0} {
		t.Errorf("the position must be applied once, got %v", created.positioned)
	}

	// 闲置销毁会把这个状态重置掉，于是下次显示重新定位。
	registry.ResetPosition("progress")
	registry.Show("progress", "")
	if created.positioned != [2]int{1920 - 560 - 32, 1080 - 160 - 32} {
		t.Errorf("after a reset the window must be positioned again, got %v", created.positioned)
	}
}

func TestRegistry_PositionFallsBackWhenWorkAreaUnknown(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(windowing.Spec{
		Options: windowing.Options{Name: "progress"},
		Policy:  windowing.Policy{Position: dockBottomRight(560, 160, 32)},
	})

	registry.Show("progress", "")
	if host.windows["progress"].options.Positioned {
		t.Error("without a work area the host should keep its own default placement")
	}
}

func TestRegistry_CreationURLCarriesTheFocusParam(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(windowing.Spec{Options: windowing.Options{Name: "progress", URL: "/?window=progress"}})

	registry.Show("progress", "/?window=progress&focus=task_1")
	if host.windows["progress"].options.URL != "/?window=progress&focus=task_1" {
		t.Errorf("expected the creation URL to carry the focus param, got %q", host.windows["progress"].options.URL)
	}
	// 已经建出来的窗口不再改地址：聚焦靠事件，重建窗口才会换地址。
	registry.Hide("progress")
	registry.Show("progress", "/?window=progress&focus=task_2")
	if host.windows["progress"].options.URL != "/?window=progress&focus=task_1" {
		t.Errorf("an existing window must keep its URL, got %q", host.windows["progress"].options.URL)
	}
}

func TestRegistry_CloseHidesAndKeepsTheWindow(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	closed := 0
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(windowing.Spec{
		Options: windowing.Options{Name: "progress"},
		Policy: windowing.Policy{OnClose: func(r *windowing.Registry) windowing.Direction {
			closed++
			r.Hide("progress")
			return windowing.CancelClose
		}},
	})

	registry.Show("progress", "")
	if cancel := host.windows["progress"].onClose(); !cancel {
		t.Fatalf("closing the progress window must be intercepted")
	}
	if closed != 1 || host.windows["progress"].visible {
		t.Errorf("the intercepted close must run its hook once and hide the window, got %d hook(s), visible=%v", closed, host.windows["progress"].visible)
	}
}

func TestRegistry_CloseAllowedWithoutPolicy(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(windowing.Spec{Options: windowing.Options{Name: "main"}})

	registry.Show("main", "")
	if cancel := host.windows["main"].onClose(); cancel {
		t.Errorf("without a close policy the close must be allowed")
	}
}

func TestRegistry_ShutdownCancelsPendingDestroys(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, 60*time.Second)
	registry.Declare(idleWindow(func() bool { return true }, func() bool { return false }))

	registry.Show("fileinfo", "")
	registry.Hide("fileinfo")
	if clock.pending() != 1 {
		t.Fatalf("expected a pending destroy, got %d", clock.pending())
	}
	registry.Shutdown()
	if clock.pending() != 0 {
		t.Errorf("shutdown must cancel pending destroys")
	}
	clock.fire()
	if host.windows["fileinfo"].closed {
		t.Errorf("a cancelled destroy must not fire")
	}
}

// 声明可以重新登记：已经建过运行时状态的窗口也要跟着新声明走，否则「覆盖登记」只是
// 写进了一个没人再读的表。
func TestRegistry_DeclareReplacesTheDeclaration(t *testing.T) {
	host, clock := newFakeHost(), &fakeClock{}
	registry := windowing.New(host, clock, time.Second)
	registry.Declare(windowing.Spec{Options: windowing.Options{Name: "main", Title: "old"}})

	registry.Show("main", "")
	if host.windows["main"].onClose() {
		t.Fatalf("without a close policy the close must be allowed")
	}

	registry.Declare(windowing.Spec{
		Options: windowing.Options{Name: "main", Title: "new"},
		Policy: windowing.Policy{OnClose: func(*windowing.Registry) windowing.Direction {
			return windowing.CancelClose
		}},
	})
	if !host.windows["main"].onClose() {
		t.Errorf("a re-declared window must pick up the new policy")
	}
}
