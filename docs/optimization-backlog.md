# 优化待办（2026-10-29 审计）

本文件记录 2026-10-29 全仓审计确认的缺陷、热路径与验证缺口，按“已在出错的事实优先”排序。

- 架构深化项仍以 `docs/architecture-backlog.md` 为准；
- 领域规则以 `CONTEXT.md` 与 `docs/adr/` 为准，本文件不复制第二份规则；
- 本文件只记问题、证据、最小修复与验收方式，状态在实施后就地更新。

## 审计范围与方法

- **范围**：Go 后端（`app*.go`、`internal/**`）、前端（`frontend/src/**`）、浏览器扩展（`extension/**`）、测试与 CI（`scripts/**`、`.github/workflows/**`）。
- **方法**：代码路径追踪、官方 API 文档核对、真实 `background.ts` 入口探针、用户实机复现。
- **基线（2026-10-29 实测）**：`go test ./...` 27 个包 514 项通过，语句覆盖率 71.8%；审计开始时工作树干净；`main` 领先 `origin/main` 11 个提交。
- **当前状态**：仅记录，未修改任何代码。

## 总表

| 编号 | 优先级 | 问题                                                    | 规模        | 状态   |
| ---- | ------ | ------------------------------------------------------- | ----------- | ------ |
| O-01 | P0     | 扩展未声明 `cookies` 权限，Cookie 转发恒为空            | 1 行 + 用例 | 已修复 |
| O-02 | P0     | 直接点击交接失败后，回退的原生下载被扩展取消            | 小          | 已修复 |
| O-03 | P0     | 媒体处理阶段无法暂停/删除，删除的任务会被写回           | 小          | 已修复 |
| O-04 | P0     | 队列锁跨越引擎取消调用，最长阻塞 5 秒                   | 小          | 待修   |
| O-05 | P1     | 分片热路径在 `coord.mu` 内写盘、克隆任务并落库          | 小—中       | 待修   |
| O-06 | P1     | 进度落盘节流判据错误，高速下载时每 60ms 全库重写        | 小          | 待修   |
| O-07 | P1     | `FileTaskStore.Save` 持锁做磁盘写；启动迁移逐条全库重写 | 小          | 待修   |
| O-08 | P1     | HLS 每个分片复制整份进度位图                            | 小          | 待修   |
| O-09 | P1     | 进度事件触发全列表排序与整树重渲染；窗口高度逐帧 IPC    | 中          | 待修   |
| O-10 | P1     | 扩展悬浮条 DOM 观察未节流；SPA 页面上下文只上报一次     | 小          | 待修   |
| O-11 | P2     | CI 未安装 ffmpeg、无超时；扩展 release 绕过门禁与测试   | 小          | 待修   |
| O-12 | P2     | 前端零组件测试；剪贴板死锁用例未接 watcher；监听器泄漏  | 小—中       | 待修   |
| O-13 | P2     | `Select` 在 `<button>` 内嵌交互元素                     | 小          | 待修   |
| O-14 | P2     | 11 个提交未推送；macOS/Linux 运行时未验证               | —           | 待处理 |

---

## P0 · 已在出错的事实

### O-01 扩展未声明 `cookies` 权限

- **现象**：扩展在 6 处调用 `chrome.cookies.getAll` 收集 Cookie 随交接转发，但 manifest 的 `permissions` 没有 `cookies`。
- **证据**：`extension/wxt.config.ts:20-28`（权限清单无 `cookies`）；`extension/entrypoints/background.ts:695`、`:739`、`:780`、`:815`、`:1319`（调用点，均被空 `catch` 包裹）；构建产物 `build/dist-extension/chrome-mv3/manifest.json` 同样缺失。Chrome 官方文档要求使用 `chrome.cookies` 必须声明 `"cookies"` 权限并提供对应 host 权限（host 权限已有 `<all_urls>`）。
- **影响**：生产环境 `chrome.cookies` 未绑定，异常被吞掉后 `cookiesStr` 恒为空。需要登录或防盗链的直链、HLS 清单与媒体请求拿不到 Cookie，表现为 403 或下载到错误内容。与 README「源网页 URL 与防盗链 Referer 补全」中的 Cookie 部分不符。
- **最小修复**：`wxt.config.ts` 的 `permissions` 增加 `'cookies'`；门禁 stage `extensionBuild` 检查构建产物 manifest 含 `cookies`；`extension/lib/handoverCookies.test.ts` 断言 Cookie 进入 `HandoverRequest.credentials.cookies`。
- **验收**：加载重新构建的扩展，在需要登录的站点触发一次交接，桌面端收到的请求上下文包含 Cookie；缺少权限时门禁在 `extensionBuild` 阶段失败。

### O-02 直接点击交接失败后，回退下载被扩展取消

- **状态**：**已实机确认并修复（2026-10-29）**。桌面端完全退出后，在普通 `<a href="/file.zip">` 上左键单击，浏览器下载被取消，文件没有落地。
- **路径**：内容脚本在 capture 阶段命中接管规则 → `preventDefault` → 发送 `DIRECT_CLICK_HANDOVER`；`background.ts:735` 在交接开始前就把 URL 记入 `recentDirectHandoverUrls`；桌面端离线时交接返回 `not_delivered`（`background.ts:1154-1160`），内容脚本用 `window.location.assign` 回退（`content.ts:203-206`）；回退产生的下载进入 `onCreated`，在 5 秒窗口内命中 `recentDirectHandoverUrls`，被无条件 `cancel` + `erase`（`background.ts:1252-1256`、`:1442-1467`）。
- **探针证据**：用真实 `background.ts` 入口 + 假 chrome、离线 `fetch` 复现：交接响应 `{"accepted":false,"failure":"not_delivered","reason":"桌面端未连接"}`，随后同 URL 的 `onCreated` 触发 `cancel(42)`。
- **范围**：仅限“点击超前拦截”这条路径。页面脚本/表单/iframe 发起的下载走 `onCreated` 拦截，离线时会 `pause` 后 `resume`，浏览器正常下载（已有用例锁定：`extension/lib/downloadIntercept.test.ts:293`、`:356`）。
- **时序风险**：取消判定依赖 5 秒窗口。若离线探测耗时超过 5 秒，回退下载会侥幸存活，同一操作的结果随机器状态变化——这本身也需要消除。
- **实际修复**：
  1. 内容脚本回退不再用 `location.assign`，改发已有的 `TRIGGER_BROWSER_DOWNLOAD`（`background.ts:359-366`，内部先 `markSelfInitiatedDownload`，`onCreated` 会直接放行），并带上 `<a download>` 给出的文件名；
  2. `handleDirectClickHandover` 返回非 `accepted` 时删除 `recentDirectHandoverUrls` 中的 URL；
  3. 新增 `extension/lib/directHandoverFallback.test.ts`：交接失败后的回退下载不得被取消；`TRIGGER_BROWSER_DOWNLOAD` 发起的下载不得被 `onCreated` 二次拦截。
- **验收**：回归用例在去掉防重表清理时超时失败、恢复后通过；用下方复现夹具，桌面端退出后点击 `file.zip`，浏览器下载正常完成；桌面端在线时仍由桌面端接管。

### O-03 媒体处理阶段无法暂停/删除

- **状态**：已修复（2026-10-29）。
- **现象**：HLS 任务传输结束进入媒体处理时，下载名额被释放，但任务唯一的取消句柄同时被删除。此时暂停不会中断处理，删除的任务会在处理结束后被重新写回。
- **证据（修复前）**：`finishTask` 在处理阶段之前释放名额并从 `activeTasks` 删除取消句柄；`Pause` 只在 `activeTasks` 中查找；收尾无条件 `store.Save`，而 `FileTaskStore.Save` 会把不存在的任务重新插入（`internal/task/store.go:102-108`）。
- **影响**：处理中按暂停，任务最终仍显示“已完成”；处理中删除，任务复活。处理失败路径的 `pausedElsewhere` 只覆盖了出错分支，成功分支没有同样的检查。
- **实际修复**：
  1. `activeTasks` 只计下载名额；新增 `executingTasks` 持有整次运行（含媒体处理）的取消句柄，`Pause` 从它取消。`schedule` 同时检查两个 map，避免同一任务出现两个运行；运行退出后清理并再次调度。
  2. 新增 `deletedTasks` 墓碑：`Delete` 在删库前标记；进度快照、中间状态与收尾写库都跳过已删除任务，避免复活与界面回弹。
  3. `finalizeRun` 收尾时重新读取存储：任务已删除则不写；用户已暂停/继续则保留用户状态，不用本次运行结果覆盖。中间状态走 `saveIfPresent`。
  4. 回归用例：`internal/engine/manager_finalize_internal_test.go` 覆盖删除不复活、墓碑阻止在途写入、处理中暂停取消运行、暂停/继续状态优先、进度快照丢弃。
- **验收**：临时去掉删除守卫时 `TestFinalizeRunDoesNotResurrectDeletedTask` 失败；去掉状态保留时 `TestFinalizeRunPreservesUserStatus` 失败；恢复后 `go test ./...` 27 个包 521 项通过。

### O-04 队列锁跨越引擎取消调用

- **现象**：文件信息窗口取消当前项时，在持有队列锁的情况下调用引擎取消预下载，最长等待 5 秒。
- **证据**：`internal/window/queue.go:833-861`（`CancelCurrent` 全程持锁，`defer qc.mu.Unlock()`）；`:859-861` 调用 `intake.CancelPreDownload`；`internal/engine/manager.go:892-907` 内部 `waitTaskIdle(taskID, 5*time.Second)`。
- **影响**：违反 ADR-0005 决策 1「队列锁不跨越引擎调用」（`docs/adr/0005-browser-extension-integration-architecture.md:33`）。取消预下载期间新的浏览器交接会在 `Enqueue` 处排队，可能超过扩展 2.5 秒的交接超时，回到“两边都拿不到下载”的失败模式。
- **最小修复**：锁内只取出 `PreDownloadTaskID` 并推进队列，锁外调用 `CancelPreDownload`（可复用窗口副作用队列或独立 goroutine）。
- **验收**：新增用例断言取消期间 `Enqueue` 不被阻塞；`CancelCurrent` 的队列语义（当前项移除、下一项推进）保持不变。

---

## P1 · 热路径与性能

### O-05 分片热路径在 `coord.mu` 内写盘、克隆任务并落库

- **证据**：`internal/engine/downloader.go:2276-2339`（每个 64KB 读取都在 `coord.mu` 内 `file.WriteAt`，随后 `publishLocked`）；`internal/engine/progress.go:66-96`（`PublishSnapshot` 在调用方持锁时克隆整个任务并同步 `store.Save`）；`internal/task/store.go:102-108`（落库为全量 JSON 序列化 + 原子写）。
- **影响**：所有分片 worker 串行在 `coord.mu` 上；每 60ms 一次的快照还会把全库 JSON 写盘放进同一临界区，高带宽/多连接时吞吐被锁和磁盘拖住。`fileMu` 只在 `coord.mu` 内被获取，没有独立作用。
- **最小修复**：`coord.mu` 只保护分片元数据与 `trackers`；`WriteAt` 移到锁外（由 `fileMu` 保护）；快照在锁内克隆、落盘与广播移到锁外。
- **验收**：用现有分片/慢区块用例与一次本机大文件下载对比吞吐；`go test -race` 覆盖分片路径。

### O-06 进度落盘节流判据错误

- **证据**：`internal/engine/progress.go:70-73`。`delta` 是“距上次发布”的字节数，而 `s.published` 每个发布窗口都会更新；`delta >= 1MB` 因此表示“这 60ms 内下载了 1MB”，不是“距上次落盘 1MB”。
- **影响**：速度超过约 17MB/s 时，每个 60ms 窗口都判定需要落盘，等于每个任务每秒 16 次全库 JSON 重写（[推算]，基于 `progressPublishInterval=60ms` 与 `progressSaveBytes=1MB`）。
- **最小修复**：新增 `lastSavedBytes`，用 `snapshot.Downloaded - lastSavedBytes >= progressSaveBytes` 判断，并只在真正落盘时更新它。
- **验收**：新增用例固定“高速连续发布只按 500ms 间隔落盘”；现有进度节流用例不回归。

### O-07 `FileTaskStore.Save` 持锁做磁盘写；启动迁移逐条重写

- **证据**：`internal/task/store.go:102-108`（持 `s.mu` 完成 `json.Marshal` 与 `atomicfile.Write`，后者在 Windows 上还带重试退避）；`app.go:518-545`（启动时对每个需要迁移的任务调用一次 `Save`）。
- **影响**：任意 `Get/List` 都会在磁盘写期间阻塞；历史任务越多，启动时的全库重写次数越多。
- **最小修复**：`Save` 在锁内更新内存并复制快照，锁外序列化与写盘（独立 `persistMu` 串行化写）；启动迁移合并为一次写盘，或在 `load` 阶段完成迁移后只写一次。
- **验收**：新增用例断言写盘期间 `Get/List` 不被阻塞（可用可控的写盘钩子）；启动迁移用例断言只发生一次持久化。

### O-08 HLS 每个分片复制整份进度位图

- **证据**：`internal/hls/download.go:518-520`（每个分片完成都 `make([]bool, len(doneFlags))` + `copy`）；`internal/engine/hls.go:393-408`（回调再复制进任务位图）。
- **影响**：数千分片的清单产生 O(N²) 布尔复制与大量短命分配，GC 压力随分片数平方增长。
- **最小修复**：位图快照按发布节流（约 60ms）生成；中间完成只上报计数与字节数。
- **验收**：用现有 HLS 用例断言进度回调仍覆盖全部分片；新增一个分片数较大的基准，观察分配次数下降。

### O-09 进度事件触发全列表排序与整树重渲染

- **证据**：`frontend/src/App.tsx:59-72`（比较器内 `new Date(...).getTime()`）；`:169-181`（每个 `TaskUpdated` 都 `sortTasks`）；`:880-896`（`TaskItem` 未 memo，回调为内联箭头函数）；`frontend/src/views/FileInfoView.tsx:707-720` 与 `frontend/src/views/ProgressView.tsx:152-160`（`ResizeObserver` 每次高度变化都直接调用 Wails `SetFileInfoWindowHeight` / `SetProgressWindowHeight`）；`app_windowing.go:362-377`（后端直接 `SetSize`）。
- **影响**：多任务下载时，后端每个任务每 60ms 一条进度事件，前端每次都做 O(N log N) 日期解析、重建列表并重排整棵树；窗口内动画期间 `ResizeObserver` 逐帧触发原生窗口 resize，造成卡顿。
- **最小修复**：`TaskItem` 用 `React.memo`，列表与筛选 `useMemo`，操作回调用 `useCallback`；排序键预先解析，或仅在 `updatedAt/createdAt` 变化时重排；窗口高度 IPC 用 `requestAnimationFrame`/尾随防抖（约 60ms）。
- **验收**：构造 50+ 任务、多个进行中的场景，观察重渲染次数与窗口动画流畅度；现有前端用例不回归。

### O-10 扩展悬浮条 DOM 观察未节流；SPA 页面上下文只上报一次

- **证据**：`extension/lib/mediabar.ts:195-200`（`MutationObserver` 监听 `document.body` 的 `childList + subtree`，每次变更同步 `scanVideos()`）；`:468-487`（`scanVideos` 每次 `querySelectorAll('video')`）；`:482` 向 `body` 插入悬浮条本身也会触发一次观察。`extension/entrypoints/content.ts:239`（`DOMContentLoaded` 上报一次页面上下文，无 `popstate`/History API 监听）。
- **影响**：弹幕、聊天、虚拟列表等高 DOM 变更页面会持续全页扫描；站内切换视频后，桌面端拿到的标题、页面 URL 与 Referer 仍是上一个视频的。
- **最小修复**：观察回调用微任务或 150–300ms 定时器合并；忽略悬浮条自身的插入；SPA 用 `popstate` + `history.pushState/replaceState` 补丁（或 `<title>` 观察）重新上报页面上下文。
- **验收**：在持续变动的页面上观察 CPU 与扫描次数；站内切换视频后交接请求的 `pageUrl`/`pageTitle`/`referer` 跟着更新。

---

## P2 · 验证与工程缺口

### O-11 CI 未安装 ffmpeg、无超时；扩展 release 绕过门禁

- **证据**：`internal/media/process_test.go:50-89`（缺少 `ffprobe`/`ffmpeg` 时 `t.Skip`）；`.github/workflows/quality.yml:32`（未安装 ffmpeg）；`quality.yml`、`release.yml` 均无 `timeout-minutes`；`.github/workflows/release-extension.yml:24-42`（仅安装依赖、`wxt zip`、上传，未跑类型检查/测试/协议镜像校验）。
- **影响**：媒体处理“独立解码校验”在 CI 永远跳过，封装正确性只剩同一套库自证；死锁或卡住的测试可以消耗到 Actions 默认上限；扩展可以在零测试状态下直接发布。
- **最小修复**：各平台 runner 安装 ffmpeg/ffprobe；为所有 job 增加 `timeout-minutes`；`release-extension.yml` 增加质量前置 job（`compile`、`bun test`、`node scripts/protocol.mjs --check`）。
- **验收**：CI 日志中不再出现媒体用例的 skip；扩展 release 在测试失败时阻断。

### O-12 前端零组件测试；剪贴板死锁用例未接 watcher；监听器泄漏

- **证据**：`find frontend/src -name '*.test.tsx'` 为 0；`frontend/src/stores/settings.ts:188-209`（`mediaQuery` 与 `theme:systemChanged` 监听没有进入返回的清理函数）；`app_test.go:1013-1028`（`TestApp_ToggleClipboardSettingsNoDeadlock` 只反复保存设置；`newTestApp` 未创建 `clipboardWatcher`，`app_settings.go:126-130` 对 nil 直接跳过）。
- **影响**：核心视图（文件信息、进度窗、设置面板）没有渲染/交互回归保护；名为“无死锁”的用例并未触及 watcher 的互斥；重复初始化会累积监听器。
- **最小修复**：为 `FileInfoView`/`ProgressView` 增加最小渲染用例（需要 `happy-dom` + `@testing-library/react`）；`initSettingsListener` 返回合并后的清理函数；在测试里注入 watcher 假实现，让死锁用例真正执行 `SetEnabled`。
- **验收**：新增用例在去掉清理函数或去掉 watcher 互斥时失败。

### O-13 `Select` 在 `<button>` 内嵌交互元素

- **证据**：`frontend/src/components/ui/Select.tsx:82`（`showClear` 时在 `DropdownMenu.Trigger` 渲染的原生 `<button>` 内放置 `<span role="button" tabIndex={0}>`）。
- **影响**：嵌套交互控件不符合 HTML/ARIA，读屏与点击事件行为不稳定。
- **最小修复**：清空按钮移到 trigger 之外，或把 trigger 改为非 button 的 `role="combobox"` 容器。
- **验收**：键盘 Tab/Enter/Space 与读屏语义正常；现有选择器行为不回归。

### O-14 交付与平台验证状态

- **证据**：`git status -sb` 显示 `main...origin/main [ahead 11]`；`docs/agents/quality.md` 明确 macOS/Linux 运行结果与远端强制执行未观察到。
- **影响**：已完成的架构深化与修复尚未推送；三平台“能构建”不等于“能运行”，托盘、窗口、更新器、剪贴板等平台差异仍未在 macOS/Linux 实机验收。
- **最小修复**：确认后再推送；按发布前检查单在 macOS/Linux 上跑一次真实操作验收（托盘驻留、窗口、更新、目录迁移、剪贴板）。
- **验收**：远端 CI 全绿；两平台实机操作记录写入发布说明或质量文档。

---

## 复现夹具（O-02）

临时目录（不在仓库内，可随时删除）：`E:\temp\sheepget-click-test\`

- `server.mjs`：本地 HTTP 服务，`/file.zip` 返回约 4MB 附件，`/note.txt` 为不在接管清单的对照，`/js-file.zip` 供 onCreated 路径对照；`/__log` 返回最近请求。
- `index.html`：普通 `<a href>` 链接、JS 触发按钮与观察步骤。
- 启动：`node server.mjs`，打开 `http://127.0.0.1:8787/`。

复现步骤：完全退出 SheepGet（含托盘）→ 打开 `chrome://downloads` → 左键单击 `file.zip`。2026-10-29 实测：下载被取消；`/__log` 可看到 `/file.zip` 请求。

---

## 已核实不属于缺陷

- **`onCreated` 路径的离线回退是正常的**：页面脚本/表单/iframe 发起的下载会先 `pause`，交接失败后 `resume`，浏览器继续下载。已有用例断言这一行为（`extension/lib/downloadIntercept.test.ts:293`、`:356`）。O-02 只针对内容脚本在 capture 阶段抢先拦截的那条路径。
- **Go 测试基线健康**：`go test ./...` 27 个包 514 项通过，覆盖率 71.8%；生产代码没有 `TODO`/`FIXME`/`nolint`/`@ts-ignore` 之类的抑制（测试里只有一处 `@ts-expect-error` 用于 mock）。
- **intake / settingsapply / 建任务入口**：2026-10-28 的四项深化已完成，当前没有发现第二份业务规则实现。

---

## 未验证与边界

- O-01 的运行时表现按 Chrome 官方权限文档与构建产物判定，未在真实浏览器里抓取交接请求核对 Cookie 是否为空。
- O-03、O-05、O-07 的修复需要并发用例与一次真实下载/暂停/删除操作共同验收；当前只有代码路径证据。
- O-06 的 17MB/s 阈值是由 60ms 发布间隔与 1MB 阈值推算，未在真实高速链路上测量落盘次数。
- O-09 的卡顿程度与任务数量、窗口动画时长相关，需要在实机上量化重渲染次数。
- macOS/Linux 的运行时行为仍未观察（O-14）。

## 执行约定

- 每项实施前先补可失败的用例，再改实现；完成后跑局部测试与相应门禁阶段，全量门禁在最终代码树上只跑一次。
- 修复提交按主题拆分（扩展权限、交接回退、引擎生命周期、持久化热路径、前端渲染、CI），不把无关项混在一个提交里。
- 本文件状态在实施后就地更新，不另建第二份清单。
