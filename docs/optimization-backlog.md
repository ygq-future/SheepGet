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
| O-04 | P0     | 队列锁跨越引擎取消调用，最长阻塞 5 秒                   | 小          | 已修复 |
| O-05 | P1     | 分片热路径在 `coord.mu` 内写盘、克隆任务并落库          | 小—中       | 已修复 |
| O-06 | P1     | 进度落盘节流判据错误，高速下载时每 60ms 全库重写        | 小          | 已修复 |
| O-07 | P1     | `FileTaskStore.Save` 持锁做磁盘写；启动迁移逐条全库重写 | 小          | 已修复 |
| O-08 | P1     | HLS 每个分片复制整份进度位图                            | 小          | 已修复 |
| O-09 | P1     | 进度事件触发全列表排序与整树重渲染；窗口高度逐帧 IPC    | 中          | 已修复 |
| O-10 | P1     | 扩展悬浮条 DOM 观察未节流；SPA 页面上下文只上报一次     | 小          | 已修复 |
| O-11 | P2     | CI 未安装 ffmpeg、无超时；扩展 release 绕过门禁与测试   | 小          | 已修复 |
| O-12 | P2     | 前端零组件测试；剪贴板死锁用例未接 watcher；监听器泄漏  | 小—中       | 已修复 |
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

- **状态**：已修复（2026-10-29）。
- **现象**：文件信息窗口取消当前项时，在持有队列锁的情况下调用引擎取消预下载，最长等待 5 秒。
- **证据（修复前）**：`internal/window/queue.go` 的 `CancelCurrent` 全程持锁，并在锁内调用 `intake.CancelPreDownload`；`internal/engine/manager.go` 的 `CancelPreDownload` 内部 `waitTaskIdle(taskID, 5*time.Second)`。
- **影响**：违反 ADR-0005 决策 1「队列锁不跨越引擎调用」（`docs/adr/0005-browser-extension-integration-architecture.md:33`）。取消预下载期间新的浏览器交接会在 `Enqueue` 处排队，可能超过扩展 2.5 秒的交接超时，回到“两边都拿不到下载”的失败模式。
- **实际修复**：`CancelCurrent` 在锁内只记录 `PreDownloadTaskID` 并完成队列推进；defer 中先解锁，再执行窗口副作用，最后调用 `CancelPreDownload`。队列语义（当前项移除、下一项推进、提交中拒绝取消）保持不变。
- **验收**：新增 `TestQueueController_CancelCurrentDoesNotHoldQueueLockWhileEngineCancels`，用 `blockingEngine("CancelPreDownload")` 卡住引擎调用，再断言 `Enqueue` 在 500ms 内返回；临时恢复锁内调用时该用例失败，恢复后 `go test ./internal/window` 49 项通过，全量 `go test ./...` 27 个包 522 项通过。

---

## P1 · 热路径与性能

### O-05 分片热路径在 `coord.mu` 内写盘、克隆任务并落库

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`internal/engine/downloader.go:2276-2339`（每个 64KB 读取都在 `coord.mu` 内 `file.WriteAt`，随后 `publishLocked`）；`internal/engine/progress.go:66-96`（`PublishSnapshot` 在调用方持锁时克隆整个任务并同步 `store.Save`）；`internal/task/store.go:102-108`（落库为全量 JSON 序列化 + 原子写）。
- **影响**：所有分片 worker 串行在 `coord.mu` 上；每 60ms 一次的快照还会把全库 JSON 写盘放进同一临界区，高带宽/多连接时吞吐被锁和磁盘拖住。`fileMu` 只在 `coord.mu` 内被获取，没有独立作用。
- **实际修复**：
  1. 读循环拆成三段：锁内只取分片元数据（`End` 与本次可写长度），`WriteAt` 只在 `fileMu` 下进行，记账、tracker 采样与完成判定再回到 `coord.mu`。写盘期间分片可能被拆分导致 `End` 收缩，越界字节属于协助分片（它会重写同一段源数据），因此按新 `End` 截断记账，进度不会越过分片总量。
  2. 发布改成两段：`snapshotFor` 在 `coord.mu` 内取走副本，`sink.PublishSnapshot` 在解锁之后执行（全库 JSON 落盘与监听者回调）。`publishLocked` 相应改为 `snapshotLocked`。
  3. `WantsSnapshot` 在肯定答复时预约这一帧（写入 `lastPublish`），使「一帧一份快照」的约束不依赖发布仍在锁内；`PublishSnapshot` 仍按已发布字节数丢弃落后的副本。
- **验收**：新增 `TestHTTPDownloader_PublishDoesNotHoldChunkLock`（第一次发布卡住时，第二个分片工作协程仍须交出自己的副本）与 `TestProgressSinkReservesOneSnapshotPerInterval`（8 个并发询问只放行 1 个）。临时把 `PublishSnapshot` 挪回锁内，前者 2 秒内失败；临时去掉 `WantsSnapshot` 的预约，后者得到 8 个肯定答复。恢复后 `go test ./internal/engine/ ./internal/hls/ ./internal/task/ -race` 187 项通过。

### O-06 进度落盘节流判据错误

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`internal/engine/progress.go` 的 `dueSave` 用 `delta >= progressSaveBytes` 判断。`delta` 是「距上次发布」的字节数，而 `s.published` 每个发布窗口都会更新；它因此表示「这 60ms 内下载了 1MB」，不是「距上次落盘 1MB」。
- **影响**：速度超过约 17MB/s 时，每个 60ms 窗口都判定需要落盘，等于每个任务每秒 16 次全库 JSON 重写（[推算]，基于 `progressPublishInterval=60ms` 与 `progressSaveBytes=1MB`）。
- **判据修正**：只把 `delta` 换成「距上次落盘」还不够。若两个阈值仍按「或」组合，33MB/s 下每 30ms 就攒够 1MB，写盘次数反而升到每秒 33 次——与「高速只按 500ms 落盘」的验收目标相反。两个常量都表述的是写盘频率上限（「最多每 500ms 或每 1MB 写一次」），因此实现为两者同时成立才写。
- **实际修复**：新增 `lastSavedBytes`；`dueSave = 首次 || (距上次落盘 ≥ 500ms && Downloaded-lastSavedBytes ≥ 1MB)`，并只在真正落盘时更新 `lastSave` 与 `lastSavedBytes`。结果是：高速（≥ 2MB/s）每 500ms 写一次，慢速每攒够 1MB 写一次（因此也 ≤ 2 次/秒），进度不动时不写。首次快照仍立即落盘；暂停、完成与失败由 `finalizeRun` / `saveIfPresent` 显式写，不受这条节流影响。
- **验收**：新增 `TestProgressSinkPersistsAtSaveIntervalDuringFastPublishing`（每 60ms 前进 2MB，跨 180ms 只写 1 次；修复前写 4 次）与 `TestProgressSinkWaitsForOneMegabyteBetweenSaves`（时间条件满足但只走了 512KB 时不写，攒够 1MB 才补一次；去掉字节门槛后该用例失败）。

### O-07 `FileTaskStore.Save` 持锁做磁盘写；启动迁移逐条重写

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`internal/task/store.go` 的 `Save` 持 `s.mu` 完成 `json.Marshal` 与 `atomicfile.Write`（后者在 Windows 上还带重试退避）；`app.go` 的 `migrateHistoricalTasksCategory` 对每个需要迁移的任务调用一次 `Save`。
- **影响**：任意 `Get/List` 都会在磁盘写期间阻塞；历史任务越多，启动时的全库重写次数越多（N 条记录 N 次 O(N) 序列化）。
- **实际修复**：
  1. 内存状态带 `revision`：`Save` / `SaveMany` / `Delete` 在锁内更新内存并取走一份可序列化快照，序列化与写盘在独立 `persistMu` 下于锁外完成。`Get` / `List` 不再等写盘。
  2. 落盘带版本守卫：`revision <= persistedRevision` 的快照直接跳过。并发写盘时后取的快照可能先落盘，没有守卫的话先取的旧快照会把磁盘写回旧内容。
  3. 新增 `TaskStore.SaveMany`；启动分类迁移把变更攒成一批，只写一次盘。写盘失败改为记录警告，不再静默丢弃。
- **验收**：新增 `TestFileTaskStoreReadsDoNotWaitForDiskWrite`（写盘卡住时 `Get`/`List` 仍立即返回；恢复锁内写盘后 2 秒超时失败）、`TestFileTaskStoreStaleSnapshotDoesNotOverwriteNewer`（去掉版本守卫后旧快照覆盖新状态，用例失败）、`TestApp_StartupMigrationPersistsOnce`（三条历史任务只发生一次批量落盘；改回逐条 `Save` 时失败）。

### O-08 HLS 每个分片复制整份进度位图

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`internal/hls/download.go` 的 `report` 在每个分片完成时 `make([]bool, len(doneFlags))` + `copy`；`internal/engine/hls.go` 的回调再 `copy` 一次进任务位图。
- **影响**：数千分片的清单产生 O(N²) 布尔复制与大量短命分配，GC 压力随分片数平方增长。
- **实际修复**：`ProgressFunc` 的最后一个参数从「整张位图」改为「自上次回调以来完成的分片序号」。进度记账收敛为 `hls.segmentProgress`（增量切片预留容量、报告后清零复用），每个分片完成只做 O(1) 追加；引擎回调按序号并入任务位图，位图只在任务快照（60ms 节流）时复制一次。续传时磁盘上已有的分片仍在第一次回调里一次带出。
- **验收**：`TestDownloadReportsProgress`（单次回调最多带一个增量）、`TestDownloadProgressDeltasCoverEverySegmentOnce`（每个分片恰好出现一次，续传起点一次带出 2 个）、`TestSegmentProgressPerSegmentWorkDoesNotScaleWithSegmentCount`（8 与 4096 分片下每分片 0 次分配；恢复整份复制后失败）、`TestRunHLSTransferMarksEverySegmentDone`（引擎位图覆盖全部分片；去掉增量应用后失败）。
- **基准说明**：`BenchmarkDownloadProgressReporting`（1024 分片，`-bench` 手动运行）保留作观察工具。端到端噪声底（±4k allocs/轮，来自 HTTP 与文件路径）大于旧实现每轮约 1k 次位图复制的增量，因此回归保护由上面的分配用例承担，不依赖基准数字。

### O-09 进度事件触发全列表排序与整树重渲染

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`frontend/src/App.tsx`（比较器内 `new Date(...).getTime()`；每个 `TaskUpdated` 都 `sortTasks`；`TaskItem` 未 memo，回调为内联箭头函数）；`frontend/src/views/FileInfoView.tsx` 与 `ProgressView.tsx`（`ResizeObserver` 每次高度变化都直接调用 Wails `SetFileInfoWindowHeight` / `SetProgressWindowHeight`）；`app_windowing.go`（后端直接 `SetSize`）。
- **影响**：多任务下载时，后端每个任务每 60ms 一条进度事件，前端每次都做 O(N log N) 日期解析、重建列表并重排整棵树；窗口内动画期间 `ResizeObserver` 逐帧触发原生窗口 resize，造成卡顿。
- **实际修复**：
  1. 列表顺序规则收进 `frontend/src/lib/taskList.ts`：`sortTasks` 先算排序键再排序（每条任务只解析一次时间）；`applyTaskUpdate` 在排序键没变时只替换那一项、不重排——进度快照不改 `UpdatedAt`，因此绝大多数事件不再触发排序，未受影响的任务项仍是同一批对象。
  2. `TaskItem` 改为 `React.memo`；App 传给它的回调全部用 `useCallback` 固定（Shift 连选需要的列表顺序经 ref 跟随，回调不再随每次进度事件换新引用）；`filteredTasks` 用 `useMemo`；任务控制回调改为非 async + `.catch`，与 `TaskItem` 的 void 回调契约一致。
  3. 窗口高度 IPC 走 `createResizeReporter`：每 60ms 合并一次并保证最后一个高度送达，两个视图的 `ResizeObserver` 不再逐帧调用原生窗口 resize。
- **验收**：新增 `taskList.test.ts`（倒序、回退创建时间、同时间按 id；排序键不变时其余项保持同一对象引用）与 `resizeReporter.test.ts`（间隔内合并、只调度一次定时器、尾随送达、dispose 丢弃）；`tsc --noEmit`、`eslint --max-warnings 0`、`vite build` 与 `vitest`（15 个文件 124 项）通过。
- **未验证**：重渲染次数与动画流畅度需要组件测试环境（`happy-dom` + `@testing-library/react`，见 O-12），本次没有在实机上量化。

### O-10 扩展悬浮条 DOM 观察未节流；SPA 页面上下文只上报一次

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`extension/lib/mediabar.ts`（`MutationObserver` 监听 `document.body` 的 `childList + subtree`，每次变更同步 `scanVideos()`；`scanVideos` 每次 `querySelectorAll('video')`；向 `body` 插入悬浮条本身也会触发一次观察）。`extension/entrypoints/content.ts`（`DOMContentLoaded` 上报一次页面上下文，无 `popstate`/History API 监听）。
- **影响**：弹幕、聊天、虚拟列表等高 DOM 变更页面会持续全页扫描；站内切换视频后，桌面端拿到的标题与页面 URL 仍是上一个视频的。
- **实际修复**：
  1. 悬浮条的 body 观察改走 `createThrottledRunner`（200ms）：首次触发立即扫描，窗口内的变更合并成一次尾随执行，持续变更不会饿死扫描。新增 `isOwnMutation`：由自己插入/移除的悬浮条宿主引起的变更不再触发扫描（宿主记在 `WeakSet` 里，移除记录异步送达时仍能认出来）。
  2. 页面上下文改为 `createPageContextReporter`：按「URL + 标题」去重；在 `popstate`/`hashchange`、`<head>` 的标题与 og:title 改写、以及 body DOM 变化（路由切换必然伴随）后重新比对，SPA 站内切换后交接请求的 `pageUrl`/`pageTitle` 跟着更新。内容脚本与页面脚本不在同一个 JS 世界，补丁 `history.pushState/replaceState` 拦不到页面自己的调用，因此没有采用该方案。
- **验收**：新增 `throttle.test.ts`（首触发立即、窗口内合并、持续变更不饿死、`cancel` 丢弃尾随）、`pageContext.test.ts`（首次上报、未变不重复、URL 或标题变化重新上报）、`mediabar.test.ts` 的 `isOwnMutation` 用例（自己的宿主不算、混合变更要扫、无节点增删交给扫描）。`bun run compile` 与 `bun run test`（18 个文件 126 项）通过。
- **顺带发现（未修）**：`content.ts` 里 `REPORT_DOM_MEDIA` 的观察仍是 300ms 纯防抖，在持续变更的页面上会被一直推迟。改成节流会带来每 300ms 一条消息的持续流量，需要与 background 侧的合并策略一起设计，留作后续。

---

## P2 · 验证与工程缺口

### O-11 CI 未安装 ffmpeg、无超时；扩展 release 绕过门禁

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`internal/media/process_test.go:50-89`（缺少 `ffprobe`/`ffmpeg` 时 `t.Skip`）；`.github/workflows/quality.yml:32`（未安装 ffmpeg）；`quality.yml`、`release.yml` 均无 `timeout-minutes`；`.github/workflows/release-extension.yml:24-42`（仅安装依赖、`wxt zip`、上传，未跑类型检查/测试/协议镜像校验）。
- **影响**：媒体处理“独立解码校验”在 CI 永远跳过，封装正确性只剩同一套库自证；死锁或卡住的测试可以消耗到 Actions 默认上限；扩展可以在零测试状态下直接发布。
- **实际修复**：
  1. `quality.yml` 与 `release.yml` 的质量 job 按 runner 安装 ffmpeg（Linux `apt`、macOS `brew`、Windows `choco`），随后执行 `ffmpeg -version` 与 `ffprobe -version`——装不上就让 job 失败，不再让媒体用例静默 `t.Skip`。
  2. 两个工作流的所有 job 增加 `timeout-minutes`（质量 30、构建 30/60、汇总 5）；`release-extension.yml` 两个 job 各 20 分钟。
  3. `release-extension.yml` 新增 `quality` 前置 job：冻结锁文件安装依赖、`node scripts/protocol.mjs --check`、`bun run --cwd extension compile`、`bun run --cwd extension test`；`build-and-release` 以 `needs: [quality]` 依赖它，测试失败时不再产出可下载的包。
- **验收**：`actionlint -shellcheck= -pyflakes=` 通过、Prettier 对 `.github` 检查通过；安装与校验步骤在本地无法执行（需要 GitHub runner），以工作流定义与静态检查为证据。CI 日志中媒体用例不再 skip、扩展 release 在测试失败时阻断，需在远端首次运行后确认。

### O-12 前端零组件测试；剪贴板死锁用例未接 watcher；监听器泄漏

- **状态**：已修复（2026-10-29）。
- **证据（修复前）**：`find frontend/src -name '*.test.tsx'` 为 0；`frontend/src/stores/settings.ts:188-209`（`mediaQuery` 与 `theme:systemChanged` 监听没有进入返回的清理函数）；`app_test.go:1013-1028`（`TestApp_ToggleClipboardSettingsNoDeadlock` 只反复保存设置；`newTestApp` 未创建 `clipboardWatcher`，`app_settings.go:126-130` 对 nil 直接跳过）。
- **影响**：核心视图（文件信息、进度窗、设置面板）没有渲染/交互回归保护；名为“无死锁”的用例并未触及 watcher 的互斥；重复初始化会累积监听器。
- **实际修复**：
  1. `initSettingsListener` 收集 `matchMedia` 的 `change`、`theme:systemChanged` 与 `SettingsUpdated` 三处监听，返回的清理函数逐个撤销；`App.tsx` 卸载时因此不再残留系统配色监听。
  2. `App.clipboardWatcher` 从具体类型改为 `clipboardWatcher` 接口（`SetEnabled`/`Stop`），`*clipboard.Watcher` 照旧满足；死锁用例注入假实现，`SetEnabled` 在自己的互斥下回读设置（与真实 `CheckOnce` 的加锁顺序一致），并断言 10 次切换全部到达监视、每次保存都在 5 秒内返回。
  3. 引入 `happy-dom` + `@testing-library/react`，新增 `ProgressView.test.tsx`（渲染进行中的下载、无任务时关闭窗口）、`FileInfoView.test.tsx`（队列项填入可编辑表单）与 `settingsListener.test.ts`（清理函数撤销全部监听）。
- **验收**：`bun run test`（18 个文件 128 项）、`tsc --noEmit`、`eslint --max-warnings 0` 与 `go test ./ -run TestApp_ToggleClipboardSettingsNoDeadlock -race` 通过；去掉清理函数时 `settingsListener.test.ts` 的 `removeEventListener` 断言失败，去掉 watcher 注入时用例不再覆盖 `SetEnabled`。

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
- O-03、O-07 的修复需要并发用例与一次真实下载/暂停/删除操作共同验收；当前只有代码路径证据。
- O-05 的锁外写盘与锁外发布已由并发用例与 `-race` 覆盖，但没有在真实高速链路上对比修复前后的吞吐。
- O-06 修复后写盘频率不再依赖 17MB/s 这个推算阈值（间隔与字节门槛都是硬约束），但仍未在真实高速链路上计数落盘次数。
- O-09 的卡顿程度与任务数量、窗口动画时长相关，需要在实机上量化重渲染次数。
- macOS/Linux 的运行时行为仍未观察（O-14）。

## 执行约定

- 每项实施前先补可失败的用例，再改实现；完成后跑局部测试与相应门禁阶段，全量门禁在最终代码树上只跑一次。
- 修复提交按主题拆分（扩展权限、交接回退、引擎生命周期、持久化热路径、前端渲染、CI），不把无关项混在一个提交里。
- 本文件状态在实施后就地更新，不另建第二份清单。
