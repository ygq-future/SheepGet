package settingsapply_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"sheep-get/internal/config"
	"sheep-get/internal/server"
	"sheep-get/internal/settingsapply"
)

// recorder 记下每次推送，测试据此断言「哪次变更到达了谁、按什么顺序」。
type recorder struct {
	calls []string
}

func (r *recorder) record(format string, args ...any) {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

func (r *recorder) drain() []string {
	calls := r.calls
	r.calls = nil
	return calls
}

type fakeStore struct {
	rec      *recorder
	snapshot config.Settings
	fail     error
}

func (s *fakeStore) Get() config.Settings { return s.snapshot }

func (s *fakeStore) Update(next config.Settings) (config.Settings, error) {
	if s.fail != nil {
		return s.snapshot, s.fail
	}
	s.rec.record("store.port=%d", next.General.ServerPort)
	s.snapshot = next
	return next, nil
}

type fakeEngine struct {
	rec      *recorder
	proxyErr error
}

func (e fakeEngine) SetMaxActiveTasks(max int) { e.rec.record("engine.max=%d", max) }

func (e fakeEngine) SetTempDirectory(dir string) { e.rec.record("engine.temp=%s", dir) }

func (e fakeEngine) SetUseServerFileTime(enabled bool) {
	e.rec.record("engine.filetime=%t", enabled)
}

func (e fakeEngine) SetProxy(mode string, addrs ...string) error {
	e.rec.record("engine.proxy=%s%v", mode, addrs)
	return e.proxyErr
}

type fakeServer struct {
	rec         *recorder
	port        int
	boundOffset int
	restartErr  error
	restarts    []int
}

func (s *fakeServer) SetTargetPort(port int) { s.rec.record("server.target=%d", port) }

func (s *fakeServer) SetAPIKey(key string) { s.rec.record("server.key=%s", key) }

func (s *fakeServer) BroadcastTakeover(sync server.TakeoverConfigSync) {
	s.rec.record("server.takeover=%d/%d", len(sync.Extensions), len(sync.ExcludedSites))
}

func (s *fakeServer) Restart(port int) error {
	s.restarts = append(s.restarts, port)
	if s.restartErr != nil {
		return s.restartErr
	}
	s.port = port + s.boundOffset
	return nil
}

func (s *fakeServer) Port() int { return s.port }

type fakeLogging struct{ rec *recorder }

func (l fakeLogging) Apply(enabled bool) { l.rec.record("logging=%t", enabled) }

type fakeClipboard struct{ rec *recorder }

func (c fakeClipboard) SetEnabled(enabled bool) { c.rec.record("clipboard=%t", enabled) }

type fakeAutostart struct{ rec *recorder }

func (a fakeAutostart) SetEnabled(enabled bool) { a.rec.record("autostart=%t", enabled) }

type fakeUpdater struct{ rec *recorder }

func (u fakeUpdater) SetProxy(mode string, addrs ...string) {
	u.rec.record("updater.proxy=%s%v", mode, addrs)
}

type fakeTheme struct{ rec *recorder }

func (t fakeTheme) SetMode(mode string) { t.rec.record("theme=%s", mode) }

type fakeWindows struct{ rec *recorder }

func (w fakeWindows) SetBackground(light bool) { w.rec.record("background=%t", light) }

func (w fakeWindows) CloseHidden() { w.rec.record("closehidden") }

type fakeEvents struct{ rec *recorder }

func (e fakeEvents) PublishSettingsUpdated(s *config.Settings) {
	e.rec.record("event.theme=%s", s.Appearance.Theme)
}

type fakeSkill struct{ rec *recorder }

func (s fakeSkill) Sync(port int, apiKey string) { s.rec.record("skill=%d/%s", port, apiKey) }

type subsystems struct {
	rec       *recorder
	engine    *fakeEngine
	server    *fakeServer
	store     *fakeStore
	logging   fakeLogging
	clip      fakeClipboard
	autostart fakeAutostart
	updater   fakeUpdater
	theme     fakeTheme
	windows   fakeWindows
	events    fakeEvents
	skill     fakeSkill
}

func newSubsystems(snapshot config.Settings) *subsystems {
	rec := &recorder{}
	s := &subsystems{rec: rec}
	s.store = &fakeStore{rec: rec, snapshot: snapshot}
	s.engine = &fakeEngine{rec: rec}
	s.server = &fakeServer{rec: rec, port: snapshot.General.ServerPort}
	s.logging = fakeLogging{rec: rec}
	s.clip = fakeClipboard{rec: rec}
	s.autostart = fakeAutostart{rec: rec}
	s.updater = fakeUpdater{rec: rec}
	s.theme = fakeTheme{rec: rec}
	s.windows = fakeWindows{rec: rec}
	s.events = fakeEvents{rec: rec}
	s.skill = fakeSkill{rec: rec}
	return s
}

func (s *subsystems) deps() settingsapply.Deps {
	return settingsapply.Deps{
		Engine:    s.engine,
		Server:    s.server,
		Logging:   s.logging,
		Clipboard: s.clip,
		Autostart: s.autostart,
		Updater:   s.updater,
		Theme:     s.theme,
		Windows:   s.windows,
		Events:    s.events,
		Skill:     s.skill,
	}
}

// initial 是一份固定的起始快照：端口与密钥都写死，断言才不会被默认值变化带着跑。
func initial() config.Settings {
	st := config.DefaultSettings("/downloads", "/temp")
	st.General.ServerPort = 9248
	st.General.APIKey = "key-a"
	return st
}

func newApplier(t *testing.T, s *subsystems) *settingsapply.Applier {
	t.Helper()
	applier, err := settingsapply.New(s.store, s.deps())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	s.rec.drain()
	return applier
}

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected pushes:\n got: %v\nwant: %v", got, want)
	}
}

// 启动那一次必须把每段都推给对应子系统，否则存下来的设置不会到达运行状态。
func TestNewPushesTheWholeSnapshot(t *testing.T) {
	s := newSubsystems(initial())
	if _, err := settingsapply.New(s.store, s.deps()); err != nil {
		t.Fatalf("New failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), []string{
		"engine.max=32",
		"engine.temp=/temp",
		"engine.filetime=false",
		"engine.proxy=system[]",
		"updater.proxy=system[]",
		"logging=false",
		fmt.Sprintf("server.takeover=%d/0", len(initial().Download.AllExtensions())),
		"server.key=key-a",
		"server.target=9248",
		"clipboard=false",
		"autostart=false",
		"theme=system",
		"background=false",
		"event.theme=system",
		"skill=9248/key-a",
	})
}

// 一次变更只推它动到的那几段：改主题不该广播接管规则，改排除站点也不该重写 Skill 文件。
func TestOnlyChangedSectionsReachSubsystems(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	// 只改一个没有子系统关心的字段：界面照样收到整份快照。
	st := s.store.Get()
	st.Appearance.AccentColor = "#123456"
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9248",
		"event.theme=system",
	})

	// 改主题：原生窗口外观与底色跟着走，接管规则与 Skill 文件不动。
	st = s.store.Get()
	st.Appearance.Theme = config.ThemeLight
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9248",
		"theme=light",
		"background=true",
		"event.theme=light",
	})

	// 改接管规则：只广播给扩展。
	st = s.store.Get()
	st.Takeover.ExcludedSites = []string{"example.com"}
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9248",
		fmt.Sprintf("server.takeover=%d/1", len(initial().Download.AllExtensions())),
		"event.theme=light",
	})
}

// 端口变更先迁移服务再落盘：服务在新端口起来了，设置里记的也是这个端口。
func TestPortChangeMovesServerBeforePersisting(t *testing.T) {
	s := newSubsystems(initial())
	s.server.port = 9248
	applier := newApplier(t, s)

	st := s.store.Get()
	st.General.ServerPort = 9300
	saved, err := applier.Update(st)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if saved.General.ServerPort != 9300 {
		t.Fatalf("expected saved port 9300, got %d", saved.General.ServerPort)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9300",
		"server.target=9300",
		"event.theme=system",
		"skill=9300/key-a",
	})
	if !slices.Equal(s.server.restarts, []int{9300}) {
		t.Fatalf("expected one restart on 9300, got %v", s.server.restarts)
	}
}

// 目标端口被占用而顺延时，设置里记的是真正绑上的端口——界面与 Skill 文件都要指向它。
func TestPortChangeRecordsThePortActuallyBound(t *testing.T) {
	s := newSubsystems(initial())
	s.server.boundOffset = 1
	applier := newApplier(t, s)

	st := s.store.Get()
	st.General.ServerPort = 9300
	saved, err := applier.Update(st)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if saved.General.ServerPort != 9301 {
		t.Fatalf("expected the bound port 9301 to be recorded, got %d", saved.General.ServerPort)
	}
	if s.store.Get().General.ServerPort != 9301 {
		t.Fatalf("expected the persisted port to be 9301, got %d", s.store.Get().General.ServerPort)
	}
}

// 端口切不过去时磁盘与运行状态都还没动：不写设置，也不推任何子系统。
func TestPortChangeFailureLeavesEverythingUntouched(t *testing.T) {
	s := newSubsystems(initial())
	s.server.restartErr = errors.New("all ports in use")
	applier := newApplier(t, s)

	st := s.store.Get()
	st.General.ServerPort = 9300
	saved, err := applier.Update(st)
	if err == nil {
		t.Fatal("expected the port failure to reach the caller")
	}
	if !strings.Contains(err.Error(), "本地端口切换失败") {
		t.Fatalf("expected a port failure message, got %v", err)
	}
	if saved.General.ServerPort != 9248 {
		t.Fatalf("expected the old port in the returned snapshot, got %d", saved.General.ServerPort)
	}
	if len(s.store.snapshot.General.APIKey) == 0 || s.store.Get().General.ServerPort != 9248 {
		t.Fatal("settings must not be persisted when the port cannot move")
	}
	assertCalls(t, s.rec.drain(), nil)
}

// 设置写不进磁盘时把服务迁回旧端口：回滚只发生在这一个地方。
func TestPersistFailureRollsThePortBack(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	s.store.fail = errors.New("disk full")
	st := s.store.Get()
	st.General.ServerPort = 9300
	if _, err := applier.Update(st); err == nil {
		t.Fatal("expected the persist failure to reach the caller")
	}
	if !slices.Equal(s.server.restarts, []int{9300, 9248}) {
		t.Fatalf("expected the server to move to 9300 and back to 9248, got %v", s.server.restarts)
	}
	if s.server.Port() != 9248 {
		t.Fatalf("expected the server back on 9248, got %d", s.server.Port())
	}
	if s.store.Get().General.ServerPort != 9248 {
		t.Fatalf("expected the stored port to stay 9248, got %d", s.store.Get().General.ServerPort)
	}
}

// 代理被拒绝时变更中止：后面的子系统都不动，界面也不会显示一份没生效的设置。
func TestProxyRejectionAbortsTheRestOfTheChange(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)
	s.engine.proxyErr = errors.New("unsupported proxy scheme")

	st := s.store.Get()
	st.Proxy.Mode = config.ProxyModeCustom
	st.Proxy.CustomAddrs = []string{"ftp://127.0.0.1:21"}
	st.General.EnableLogging = true
	if _, err := applier.Update(st); err == nil {
		t.Fatal("expected the proxy rejection to reach the caller")
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9248",
		"engine.proxy=custom[ftp://127.0.0.1:21]",
	})

	// 基准没有前进：下一次变更会重新尝试代理，而不是把它当成已经生效。
	s.engine.proxyErr = nil
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	calls := s.rec.drain()
	if len(calls) < 2 || calls[1] != "engine.proxy=custom[ftp://127.0.0.1:21]" {
		t.Fatalf("expected the proxy to be retried, got %v", calls)
	}
	if !slices.Contains(calls, "logging=true") {
		t.Fatalf("expected the rejected change to be pushed after the retry, got %v", calls)
	}
}

// 重启端口是明确动作：即使端口没变也要重新绑定，但不该因此重写设置文件。
func TestRestartPortOnTheSamePortSkipsPersistence(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	bound, err := applier.RestartPort(9248)
	if err != nil {
		t.Fatalf("RestartPort failed: %v", err)
	}
	if bound != 9248 {
		t.Fatalf("expected 9248, got %d", bound)
	}
	if !slices.Equal(s.server.restarts, []int{9248}) {
		t.Fatalf("expected the server to be restarted, got %v", s.server.restarts)
	}
	assertCalls(t, s.rec.drain(), nil)
}

// 重启后顺延到相邻端口时，设置与 Skill 文件都跟着新端口走。
func TestRestartPortRecordsTheBoundPort(t *testing.T) {
	s := newSubsystems(initial())
	s.server.boundOffset = 2
	applier := newApplier(t, s)

	bound, err := applier.RestartPort(9400)
	if err != nil {
		t.Fatalf("RestartPort failed: %v", err)
	}
	if bound != 9402 {
		t.Fatalf("expected the bound port 9402, got %d", bound)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9402",
		"server.target=9402",
		"event.theme=system",
		"skill=9402/key-a",
	})
}

// 启动时端口顺延：只在真的与设置不一致时才写回。
func TestAdoptPortPersistsOnlyOnDrift(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	if err := applier.AdoptPort(9248); err != nil {
		t.Fatalf("AdoptPort failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), nil)

	// 启动时服务已经绑在顺延后的端口上，adopt 只是把它记下来。
	s.server.port = 9301
	if err := applier.AdoptPort(9301); err != nil {
		t.Fatalf("AdoptPort failed: %v", err)
	}
	assertCalls(t, s.rec.drain(), []string{
		"store.port=9301",
		"server.target=9301",
		"event.theme=system",
		"skill=9301/key-a",
	})
}

// 轻量模式只在刚打开那一刻收热备窗口；之后改别的设置不该顺手销毁窗口。
func TestLightweightModeClosesHiddenWindowsOnlyWhenTurnedOn(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	st := s.store.Get()
	st.General.LightweightMode = true
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if calls := s.rec.drain(); !slices.Contains(calls, "closehidden") {
		t.Fatalf("expected hidden windows to be closed when lightweight mode turns on, got %v", calls)
	}

	st = s.store.Get()
	st.Appearance.AccentColor = "#abcdef"
	if _, err := applier.Update(st); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if calls := s.rec.drain(); slices.Contains(calls, "closehidden") {
		t.Fatalf("expected unrelated changes to leave windows alone, got %v", calls)
	}
}

// SyncSkill 用运行时真正绑定的端口，而不是设置里那个还不一定绑得上的值。
func TestSyncSkillUsesTheBoundPort(t *testing.T) {
	s := newSubsystems(initial())
	applier := newApplier(t, s)

	s.server.port = 9311
	applier.SyncSkill()
	assertCalls(t, s.rec.drain(), []string{"skill=9311/key-a"})
}

// 接管规则只有一份翻译：末尾全集来自分类，排除站点与快捷键来自接管设置。
func TestTakeoverSyncTranslatesSettings(t *testing.T) {
	st := initial()
	st.Takeover.ExcludedSites = []string{"example.com"}
	st.Takeover.PauseShortcut = "F8"
	st.Takeover.ForceShortcut = "F9"

	sync := settingsapply.TakeoverSync(&st)
	if !slices.Equal(sync.Extensions, st.Download.AllExtensions()) {
		t.Fatalf("expected extensions from the category rules, got %v", sync.Extensions)
	}
	if !slices.Equal(sync.ExcludedSites, []string{"example.com"}) {
		t.Fatalf("expected excluded sites to pass through, got %v", sync.ExcludedSites)
	}
	if sync.PauseShortcut != "F8" || sync.ForceShortcut != "F9" {
		t.Fatalf("expected shortcuts to pass through, got %q/%q", sync.PauseShortcut, sync.ForceShortcut)
	}
	if settingsapply.TakeoverSync(nil).Extensions != nil {
		t.Fatal("nil settings must translate to an empty sync payload")
	}
}
