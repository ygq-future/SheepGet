# 架构优化执行顺序（2026-09-23 评审结论）

本文件记录本次架构评审得出的 8 项深化候选及其**建议执行顺序**，供后续逐项探讨与实施。
评审全文（含前后对比图与证据）：`E:\temp\architecture-review-20260923-144554.html`。

用词遵循 `codebase-design` 的词汇：模块 / 接口 / 实现 / 深度 / 接缝 / 适配器 / 杠杆 / 局部性。

## 排序原则

1. **正确性优先**：已在出错、或已违反既有契约的项排在维护性收益之前。
2. **风险与范围**：范围可控、可独立验证的先做；同文件改动串行，避免互相踩踏。
3. **依赖顺序**：后续项依赖的能力先落地（例如入口收拢依赖落点预留模块）。
4. **发版时点**：v1.0.0 发布在即，直接影响发版事故概率的项提前。

## 执行顺序总览

| 序 | 编号 | 项目 | 强度 | 规模 | 状态 |
|---|---|---|---|---|---|
| 1 | C2 | 进度只给快照，不再共享同一个 `*task.Task` | 强 | 小—中 | 待探讨 |
| 2 | C8 | 版本一致性：做成检查，而不是清单 | 值得探索 | 小 | 待探讨 |
| 3 | C5 | 「这个名字被占了」只保留一条规则 | 强 | 小—中 | 待探讨 |
| 4 | C4 | 给环回通信契约一个归属模块 | 强 | 中 | 待探讨 |
| 5 | C1 | 让下载入口只有一个归属模块（含删死代码） | 强 | 大 | 待探讨 |
| 6 | C3 | 窗口生命周期收成一个模块，每窗口一份声明 | 强 | 中 | 待探讨 |
| 7 | C7 | 为扩展 Service Worker 立一条「就绪」接缝 | 值得探索 | 中 | 待探讨 |
| 8 | C6 | 把目标落点记在任务上，不让界面重推一遍 | 值得探索 | 中—大 | 待探讨 |

依赖关系（箭头 = 前者是后者的前置）：

```
C5 ──▶ C1 ──▶ C3
C4 ──▶ C1
C4 ──▶ C7
C1 ──▶ C6（且 C6 需先定「历史归属」语义）
C2 · C8 独立，无前置
```

---

## 1. C2 进度只给快照，不再共享同一个 `*task.Task`

- **强度**：强（唯一「已经在出错」的项）
- **状态**：已完成（2026-09-23），待用户验收；门禁通过，未提交
- **问题**：`chunkCoordinator` 与 `Manager` 持有同一个 `*task.Task`；分片在工作协程里持续被改写，而 Wails 在另一条 goroutine 上、不持任何锁地序列化同一份指针。
- **证据**：
  - `internal/engine/downloader.go`（coordinator 持同一指针并按 `coord.mu` 改分片）
  - `internal/engine/manager.go`（`progressCb` → `store.Save(bgCtx, t)` 与 `notify(t)`）
  - `app.go:282-286` → Wails `EventProcessor.Emit` → `internal/mailbox/mailbox.go:26-29`（新起 goroutine）→ `webview_window.go:1448`（`json.Marshal`）
  - 门禁里的 `go test` 不使用 `-race`，事件路径在测试中不产生使载荷被并发序列化的情形，因此原有测试长期为绿。
- **实际改动**（按「一个事实一个写方」的原则落成三条不变式）：
  1. **传输期间任务的可变对象只属于传输侧**：`Manager.runTask` 把 `t.Clone()` 作为工作副本交给传输，传输结束后一次性 `*t = *work` 合并回持久对象（分片进度、ETag、大小一并带回，失败与暂停路径保存的仍是最新分片状态）。
  2. **对外发布的永远是自有副本**：`Manager.notify` 自己拷一份；传输期间改由 `TransferSink`（`WantsSnapshot` / `PublishSnapshot`）在传输侧的一致性锁内取快照，`internal/engine/progress.go` 里的 `progressSink` 统一承载 60ms 界面节流、500ms/1MB 落盘节流与速度采样。
  3. **一个「任务副本」只有一处定义**：新增 `task.Task.Clone()`，`FileTaskStore` 的 `Save/Get/List` 改用它，删掉原先三处手写的浅拷贝。
  - 顺带收敛的接口：`ProgressFunc(downloaded, chunkIndex, chunkDownloaded)` 中后两个参数无任何消费者，改为单一出口 `TransferSink`；HLS 侧字节数不再前进时也汇报（分片计数在变）。
- **未做**：`SetMinSplitETA` 保留。它是 `slow_test.go` 让慢块拆分快速发生的必要开关，删掉会让慢块测试多等数秒；报告里「顺带删除」的说法不成立，此处更正。
- **验证**（实际执行）：
  - 新增 `internal/engine/progress_publish_test.go`：监听者在传输期间于独立 goroutine 反复 `json.Marshal` 载荷（复刻 Wails 的派发方式），并断言已发布载荷之后不再变化。
  - 修复前（HEAD + 该用例，独立 worktree 中运行）：14 次 `WARNING: DATA RACE`，且断言失败（`status = "completed"`、`downloaded = 2097152`）。
  - 修复后：`go test -race -count=1 ./internal/engine/` 通过（7.5s），`go test -race ./internal/engine/ ./internal/task/ ./internal/hls/ ./internal/media/` 通过。
  - `node scripts/quality-gate.mjs` 全绿（Go 全包测试、前端 88 项、扩展 79 项、构建与基建检查）。
- **风险（已核对）**：事件载荷结构未变，前端无需改动；`*t = *work` 覆盖整份任务，避免逐字段合并漏字段。
- **遗留发现**：`internal/window` 在 `-race` 下仍报一次竞态，位置在测试文件自身（`queue_hls_test.go:63` 的 fake 引擎字段写 vs `queue_hls_test.go:379` 的测试读），与本次改动无关，属测试未同步异步调用；门禁不含 `-race` 故长期未暴露。归入 C1/C3 的测试面一起处理。

## 2. C8 版本一致性：做成检查，而不是清单

- **强度**：值得探索（发布在即，收益直接）
- **问题**：桌面端元数据、安装包、扩展清单与文档的版本一致性目前是七处手改文件 + 文字清单，质量门禁无任何阶段能发现漏改。
- **证据**：`build/config.yml:8`、`package.json:3`、`build/windows/info.json:3,7`、`extension/package.json:4`、`extension/wxt.config.ts:15`、`.github/workflows/release.yml:12`、`README.md:102-104`、`scripts/package.mjs:26-34`、`AGENTS.md` §版本协同与发版核对红线。
- **深化方向**：一个版本模块（`--check` 接入门禁，`--set <v>` 一次性改写全部来源），保留 `build/config.yml` 为 SSOT。
- **影响面**：`scripts/`、`.github/workflows/`、`AGENTS.md` 相关章节。
- **前置**：无。
- **验证**：故意改错一处后 `--check` 必须失败；`--set` 后七处一致。
- **风险**：`scripts/package.mjs` 现有正则解析需与新版保持一致，避免出现两套版本读取实现。

## 3. C5 「这个名字被占了」只保留一条规则

- **强度**：强（正确性相邻，且是 C1 的前置）
- **问题**：三个模块用三种口径回答「目标名字是否被占 / 下一个序号副本是什么」，而真正发名字的队列对自己已发给排队项的命名是盲的——两个排队项可能建议同一个名字。
- **证据**：`internal/engine/manager.go:107-136`（只看磁盘）、`manager.go:616-635`（磁盘 + 任务库）、`app.go:664-706`（磁盘 + 任务库 + 队列项）、`internal/window/queue.go:349-354, 572-576`（只调前两者）。
- **深化方向**：目标落点预留收成一个模块，接收「占用来源」（磁盘 / 任务 / 排队项）并提供 `taken` 与 `suggest`；入口、重复链接裁决与文件信息窗口读同一份答案。
- **影响面**：`internal/engine`、`internal/window/queue.go`、`app.go`、`internal/duplicate`。
- **前置**：无（但它是 C1 的前置）。
- **验证**：两个排队项先后登记同一目标名，建议名必须不同；序号副本与冲突提示结果一致。
- **风险**：`app.go:CheckURLFilesExist` 的跨目录复用语义与「占用」相关但不等价，收拢时不要把它一并改写。

## 4. C4 给环回通信契约一个归属模块

- **强度**：强
- **问题**：端点路径、鉴权头、事件名、端口与顺延步长、载荷结构、固定扩展 ID 被写了两遍且无归属；两边「契约测试」各自钉住自己那份字面量，扩展与桌面端可静默漂移。
- **证据**：`internal/server/server.go:296-306, 366-393`、`internal/server/types.go`、`internal/config/model.go:111`、`internal/events/events.go`、`extension/lib/client.ts:10-11, 22-75, 246-249`、`extension/lib/types.ts`、`frontend/src/lib/constants.ts`、`frontend/src/lib/events.ts`、`internal/server/server_test.go:364-368`、`extension/lib/client.test.ts:11-16`（9248 出现在 3 个代码文件，顺延步长 `+5` 出现在 2 个）。
- **深化方向**：一个 protocol 模块持有 Go 侧线上事实，生成 TypeScript 镜像，门禁在两侧不一致时失败。ADR-0006 已定通道，本项不触碰该决策。
- **影响面**：`internal/server`、`internal/events`、`extension/lib`、`frontend/src/lib`、`scripts/quality-gate.mjs`、`scripts/package.mjs`（扩展产物）。
- **前置**：无（但它是 C1、C7 的前置）。
- **验证**：改一处端点或事件名，未同步的一侧必须让门禁失败。
- **风险**：生成器引入构建步骤，需确保扩展与前端各自可独立构建，不破坏现有门禁阶段划分。

## 5. C1 让下载入口只有一个归属模块（含删死代码）

- **强度**：强
- **问题**：凭据/Cookie/Referer 整理、链接刷新、取消语义、提交生命周期在 `app.go` 三个处理器里各写一遍，又与 `QueueController` 形成只靠注释维持的口头协议；早期「提前下载」流程留下的 5 个绑定方法仍在发布面上（前端 0 调用）。
- **证据**：`app.go:788-827`、`app.go:889-893`、`app.go:936-1035`（三处重复的 headers 组装）、`app.go:1041-1118`、`frontend/bindings/sheep-get/app.ts`（62 个绑定函数）。
- **深化方向**：交接 + 队列 + 提交收敛为一个 intake 模块（`Handover` / `Enqueue` / `Submit` / `Cancel` / `SelectVariant`），删除 5 个已死转发；`App` 只保留绑定、设置与系统外壳适配。
- **影响面**：`app.go`、`internal/window/queue.go`、`internal/server` 适配层、`frontend/bindings`（重新生成）。
- **前置**：C5（落点预留）、C4（契约模块）。
- **验证**：交接 → 文件信息窗口 → 提交 → 取消 全链路行为不变；`app_test.go` 中锚定已死流程的用例按新契约重写或删除。
- **风险**：改动面最大，且触及 Wails 绑定面；建议分两步——先删死代码与收敛重复（行为不变），再移入 intake 模块。

## 6. C3 窗口生命周期收成一个模块，每窗口一份声明

- **强度**：强
- **问题**：三个窗口的创建 / 显示 / 定位 / 关闭钩子 / 闲置销毁分别写在三个方法里，另有每窗口一个 timer 字段与两处 `switch name`；`app_test.go:1143` 记录无头环境 `getApp()` 为 nil，这段策略目前无验证路径。
- **证据**：`app.go:65-160`、`app.go:1121-1160`、`app.go:1228-1410`、`app_test.go:1140-1148`。
- **深化方向**：窗口注册表模块持有声明与策略，唯一适配器是 Wails 视图；宽限时长、「队列非空不销毁」、关闭即取消留在实现内，可注入假时钟。
- **影响面**：`app.go`、`app_test.go`。
- **前置**：C1（先让 `app.go` 变薄，避免同文件大改并行）。
- **验证**：闲置销毁策略、位置计算、关闭钩子语义单测覆盖；三个窗口行为与现状一致。
- **风险**：Wails v3 Beta 的钩子取消语义在 macOS/Linux 未实测，抽模块时不要假设其等价性。

## 7. C7 为扩展 Service Worker 立一条「就绪」接缝

- **强度**：值得探索
- **问题**：只要下载事件在未 await 的 `init()` 读完存储之前到达，接管判定就会对着后缀清单为空的默认配置做出；失败静默（下载留在浏览器完成）。
- **证据**：`extension/entrypoints/background.ts:41, 94, 225, 890-915`、`extension/lib/storage.ts:12-22`、`extension/lib/handover.test.ts`（仅覆盖纯函数）。
- **深化方向**：配置、会话与媒体池收进一个带就绪保证的状态模块，入站事件统一 `await ready()` 后再判定。
- **影响面**：`extension/entrypoints/background.ts`、`extension/lib/storage.ts`、`extension/lib/handover.ts`。
- **前置**：C4（就绪语义若影响线上契约，需在契约模块内表达）。
- **验证**：冷启动即触发下载事件的场景下，判定必须使用已恢复的规则；现有纯函数测试保留。
- **风险**：Chromium 挂起阈值不可控，属于概率性竞态；以「不变式在模块内成立」为准，不以复现率证明。

## 8. C6 把目标落点记在任务上，不让界面重推一遍

- **强度**：值得探索
- **问题**：CONTEXT.md 写明目标落点由后端判定一次、界面读取结果，但 `task.Task` 上没有命中分类字段，主列表对每个任务、每次渲染都要跑一遍 TypeScript 版的优先级规则。
- **证据**：`frontend/src/lib/category.ts:51-99`、`frontend/src/lib/taskFilter.ts:18`、`frontend/src/App.tsx:201`、`frontend/src/category.test.ts:23-134`、`internal/task/task.go:41-72`、`internal/config/model.go:544-596`。
- **深化方向**：入口解析一次落点并把命中分类记在任务上，界面按事实筛选。
- **影响面**：`internal/task`、`internal/engine`、`internal/config`、`frontend/src/lib`、`frontend/src/App.tsx`、`frontend/bindings`（重新生成）。
- **前置**：C1（在入口一次解析）。
- **待定语义**：用户后来修改分类规则时，历史任务上记录的分类算「历史归属」还是「按现行规则重算」——先定这个再动手。
- **验证**：侧栏分类计数与列表筛选一致；改动分类规则后的行为与选定语义一致。

---

## 附加项：传输尾部健壮性与可观测性（诊断驱动，2026-09-23）

**来源**：C2 验收过程中的真实现象——一次 372.86 MiB 的文件在 97% 处爬行约 3 分钟才完成（末段约 60 KB/s，前面均速约 1 MB/s）。分片表显示慢区被反复拆成 33 个辅助分片（最小 74 KB），说明客户端一直在推进、是那一带的字节取不快。据此确认两个可改的缺口，并补上事后能回答「网络还是引擎」的日志。

- **状态**：已实现，待验收；门禁通过，未提交

### A. 停滞看门狗（连接静默挂住）

- **问题**：HTTP 客户端 `Timeout: 0`，分片请求只受任务 ctx 控制；对端保持连接却不发数据时不产生任何错误，读会一直阻塞。动态拆分只覆盖剩余 ≥128 KB 的情形，尾部 <128 KB 挂在死连接上时无任何自动恢复。
- **改动**：新增 `internal/stallwatch`（读看门狗：连续 N 秒无字节 → 取消该次请求，区分「停摆」与「父 ctx 取消」）。引擎的分片与单流传输、HLS 的每个请求都接上它；HLS 原先私有的 `idleReader` 收敛到同一实现（公开契约 `hls.ErrStalled` / `IdleTimeout` / `SetIdleTimeoutForTest` 保持不变）。引擎侧新增 `DefaultStallTimeout = 30s`，`SetStallTimeout` 供调优与测试。
- **行为变化**：静默挂住的连接在 30 秒内被斩断，传输层从当前偏移重连（HLS 侧走它既有的分片重试）；单流没有断点可续，停摆按失败上报而不是无限等待。

### B. 重试退避

- **问题**：重试间隔固定 300ms 且无上限，被限速的站点会被越推越远（429 只会更多）。
- **改动**：指数退避 300ms → 600ms → … → 封顶 10s（`retryBackoff`）；某次尝试有字节进展就把退避拉回起点。
- **行为变化**：持续失败时重试频率下降；网络恢复后仍能及时续上。

### C. 请求编年史 + 滚动日志

- **问题**：`%APPDATA%\SheepGet\logs` 为空，出问题后无法回答「服务端慢还是本机网络慢」。
- **改动**：新增 `internal/logging`（`log/slog` + 按大小滚动的文件写入器：单文件 2 MiB、最多 5 份，历史有上限；`SafeURL`/`SafeError` 去掉 query/fragment/userinfo 与错误文本里的地址，日志不落令牌）。引擎记请求级与任务级事实：chunked/single/HLS 传输的开始与结束（字节、耗时、请求数、辅助分片数）、每次请求完成/失败重试（含 `stalled` 标记）、慢块拆分、任务收尾（状态、失败阶段、错误、耗时）。日志写入不在任何分片锁内进行。
- **开关**：设置 → 系统行为 → 「记录运行日志」，**默认关闭**（`GeneralConfig.EnableLogging`）。关闭时不打开也不创建任何文件；打开时按需创建 `logs/` 与当前文件，并把出口接到引擎；库不可写时降级为丢弃日志，不影响下载。
  - 出口存放为原子指针：开关在设置线程切换，读它的地方分散在启动、退出与环回回调里。

### D. 日志纳入清理功能

- **改动**：清理弹窗新增「清理历史运行日志」一栏，复用既有的天数条件（30 天 / 90 天 / 180 天以前）。扫描结果给出文件数与占用，计入总计的「文件数 / 可释放空间」；执行时按同一条件删除。
- **规则（引擎内唯一判定）**：只认 `internal/logging.ListLogFiles` 认领的文件（`sheepget.log` 与 `sheepget.N.log`），**正在写入的那一份永不参与**，目录里的其它文件一概不碰；日志目录不存在时按「没有日志」处理。
- **分工**：目录与「哪一份正在写」由 `App` 注入（`fillLogCleanupTarget`），「多旧算旧、哪些文件算日志」留在引擎。

### 验证记录

- 新增测试：`internal/stallwatch`（停摆、慢但活跃、父取消优先、响应头阶段、Stop 释放请求 ctx）、`internal/logging`（轮转与保留上限、记录不被截断、轮转失败后仍可写、并发写入完整、丢弃出口、`SafeURL`/`SafeError` 脱敏）、`internal/engine/stall_test.go`（服务器发一半就停住：必须重连并产出完整文件，且日志里有 `stalled=true`）、`internal/engine/backoff_internal_test.go`（退避增长与封顶）。
- 反证：关掉看门狗（超时设为 1 小时）后同一场景**永久阻塞**，确证用例抓的是真问题。
- 代码评审发现并已修（各附一条回归测试）：
  1. **日志轮转失败会把日志永久写死**：改名失败时文件已关闭且未重开，之后每条日志都以「文件已关闭」失败，下次轮转又在关闭已关闭文件时立刻放弃。改为无论改名成败都重新打开当前文件（退回继续追加）。回归测试在修复前失败：`write 1 failed ...: file already closed`。
  2. **`Watch.Stop` 未取消请求 ctx**：一次大文件传输有成百上千次请求，不取消就会把死节点一路挂在父 ctx 上。改为 Stop 同时取消。
  3. **错误文本里的地址未脱敏**：`net/http` 的 `*url.Error.Error()` 会带上完整地址（含查询串令牌），原先直接进日志与任务错误信息。新增 `logging.SafeError`，在引擎两条传输路径与 HLS 的请求出口统一脱敏。
- 命令：`go test -count=1 ./internal/...` 全通过；`go test -race ./internal/engine ./internal/stallwatch ./internal/logging ./internal/hls` 通过；`node scripts/quality-gate.mjs` 全绿。
- 开关与清理的测试：`TestApp_LoggingSwitch`（关闭时不落盘 → 打开后写文件 → 再关闭后停止写入且不再持有路径）、`TestCleanup_OldLogs`（40 天前的两个轮转文件被列出并删除，当前文件与无关文件保留，总计计入文件数与字节）、`TestCleanup_OldLogsWithoutLogDir`（日志目录不存在时照常扫描）、`TestListLogFiles_OnlyOwnLogs` / `MissingDirIsEmpty`、`TestDefaultSettings` 的默认关闭断言、前端 `DEFAULT_CLEANUP_CONFIG.oldLogsEnabled` 断言。
- 待确认（未做）：probe（探测）请求仍未接看门狗——一次挂住的探测会拖住该次交接，但不影响已在传输的任务；是否补上与 A 同类，留待决定。

---

## 执行约定

- 每项开始前先按仓库协作边界给出理解、方案、影响面，取得确认后再改代码。
- 每项完成后跑完整质量门禁，并逐条过 `docs/agents/post-implementation-checklist.md`。
- 质量门禁通过后交由人工验收，未经明确许可不提交、不推送。
- 顺序可因实际情况调整；若某项在探讨中发现前置关系变化，回到本文件更新顺序与依赖图。
