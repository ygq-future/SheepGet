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

| 序  | 编号 | 项目                                      | 强度     | 规模  | 状态   |
| --- | ---- | ----------------------------------------- | -------- | ----- | ------ |
| 1   | C2   | 进度只给快照，不再共享同一个 `*task.Task` | 强       | 小—中 | 已完成 |
| 2   | C8   | 版本一致性：做成检查，而不是清单          | 值得探索 | 小    | 已完成 |
| 3   | C5   | 「这个名字被占了」只保留一条规则          | 强       | 小—中 | 已完成 |
| 4   | C4   | 给环回通信契约一个归属模块                | 强       | 中    | 已完成 |
| 5   | C1   | 让下载入口只有一个归属模块（含删死代码）  | 强       | 大    | 已完成 |
| 6   | C3   | 窗口生命周期收成一个模块，每窗口一份声明  | 强       | 中    | 已完成 |
| 7   | C7   | 为扩展 Service Worker 立一条「就绪」接缝  | 值得探索 | 中    | 已完成 |
| 8   | C6   | 把目标落点记在任务上，不让界面重推一遍    | 值得探索 | 中—大 | 已完成 |

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
- **状态**：已完成（2026-09-23），门禁通过
- **问题**：桌面端元数据、安装包、扩展清单与文档的版本一致性目前是七处手改文件 + 文字清单，质量门禁无任何阶段能发现漏改。
- **证据**：`build/config.yml:8`、`package.json:3`、`build/windows/info.json:3,7`、`extension/package.json:4`、`extension/wxt.config.ts:15`、`.github/workflows/release.yml:12`、`README.md:102-104`、`scripts/package.mjs:26-34`、`AGENTS.md` §版本协同与发版核对红线。
- **实际改动**：
  - 新增 `scripts/version.mjs`：`build/config.yml` 的 `info` 块是 SSOT，模块内一个极简块解析器同时给出取值与取值区间，`scripts/package.mjs` 改用同一解析（原 30-40 行的正则块删除），不存在第二份版本读取实现。
  - `MIRRORS` 表一次性声明全部镜像；每条声明用带 `d` 标记的正则捕获裸版本号，`--set` 只改写捕获区间（引号、`v` 前缀、产物名原样保留），`--check` 要求每条声明至少命中一次且取值等于 SSOT——声明被改名或删除会报「no declaration」，而不是静默放过。
  - 镜像清单：根 `package.json`、`build/windows/info.json`（`file_version` 与 `ProductVersion`）、扩展 `package.json`、`extension/wxt.config.ts`、扩展 popup 页脚版本显示、`.github/workflows/release.yml` 的三处 `v1.0.0`、`README.md` 的三个安装包命名示例。
  - 评审证据里漏掉的两处一并纳入：`internal/server/server.go` 原先在发现响应与 ping 响应各写一遍 `"1.0.0"`，改为唯一常量 `internal/version.Version`（ping 载荷改为包级私有变量，顺带去掉了逐请求拼接）；扩展 popup 的 `v1.0.0` 同样纳入镜像。
  - 版本号格式固定为 `X.Y.Z`（Windows PE 资源、MSI `ProductVersion` 与 Chrome manifest 的共同约束），`--check` 与 `--set` 都拒绝其它写法。
  - 门禁新增 stage `version`；原先的工具版本检查函数改名 `toolVersions` 以免与产品版本混淆。
  - `AGENTS.md` 的七处手改清单改写为模块说明（清单不再重复维护），`docs/agents/quality.md` 增加能力矩阵行与检查范围说明。
- **验证**（实际执行）：
  - `node scripts/quality-gate.mjs` 全绿（Go 全包、前端 88 项、扩展 79 项、8 项质量设施测试，`internal/version` 无测试文件属预期）。
  - 漂移反证：把根 `package.json` 改成 `"version": "1.1.0"` 后，`node scripts/version.mjs --check` 与 `node scripts/quality-gate.mjs --stage version` 均以 `package.json:3: version is 1.1.0, expected 1.0.0` 失败；还原后重新通过。
  - `--set` 反证：真实工作树上 `--set 9.9.9` 一次改写 9 个文件（清单见输出），`--set 1.0.0` 后 `git diff` 无任何版本相关残留；非法版本 `1.0` 被拒绝且不写入任何文件。
  - 质量设施新增用例 `version check rejects drifted copies and --set rewrites every source`：复制真实版本文件到临时目录后验证漂移阻断、非法版本拒绝、全量改写结果（`v` 前缀、popup 显示串、README 产物名、Go 常量）与真实仓库未被触碰。
- **影响面**：`scripts/version.mjs`（新增）、`scripts/quality-gate.mjs`、`scripts/package.mjs`、`internal/version`（新增）、`internal/server/server.go`、扩展 popup、`AGENTS.md`、`docs/agents/quality.md`。
- **风险**：一次 `--set` 会同时改动扩展与用户文档，扩展独立发版时须与桌面端同批提交；HTTP 载荷结构未变，扩展与前端无需改动；`build/windows/installer/project.nsi` 里那行 `## !define INFO_PRODUCTVERSION "1.0.0"` 是 Wails 上游模板注释（非本项目元数据），刻意不纳入镜像。

## 3. C5 「这个名字被占了」只保留一条规则

- **强度**：强（正确性相邻，且是 C1 的前置）
- **状态**：已完成（2026-09-23），门禁通过
- **问题**：三个模块用三种口径回答「目标名字是否被占 / 下一个序号副本是什么」，而真正发名字的队列对自己已发给排队项的命名是盲的——两个排队项可能建议同一个名字。
- **证据**：磁盘-only 的 `engine.CheckFileConflict`、磁盘＋任务库的 `Manager.NumberedCopyName`、磁盘＋任务库＋队列项的 `App.isFilenameTaken`，以及 `internal/window/queue.go` 三处「先取磁盘答案、再用另一处覆盖建议」的组装。
- **实际改动**：
  - 新增 `internal/engine/occupancy.go`（目标落点占用判定）：三类来源——磁盘（含 `<name>.sheepget` 中转文件）、同目录同名且正在进行中的任务、排队项已经发出的名字——收成一个 `Occupancy` 值，答案只有两个：`Taken`（是否被占）与 `Suggest`（可用名）。`NumberedCopy` 是同一判定下「序号副本」动作的名字：它永远给编号名，与「原名可用时就是原名」的建议语义不同（两者都由同一份占用判定得出，因此冲突提示与序号副本不再互相矛盾）。
  - 原先那三份实现全部删除（`CheckFileConflict`、`Manager.NumberedCopyName`、`App.isFilenameTaken`），四处组装点改为读同一份判定。
  - 队列从「只读磁盘与任务库」变成知道自己已经发出的名字：`reservedNamesLocked` 把其它项的 `Filename` 与 `SuggestedFilename` 取成快照判定器（两项都可能被提交），快照离开队列锁后仍可用，占用判定不会回头拿队列锁。
  - 正在编辑的那一项排除在外：窗口里的冲突提示回答的是「除了它自己，还有谁占着」。此前 App 侧会把这一项自己的名字算成自己的冲突（改链接后重新探测就撞上），队列侧则因为不含队列项而恰好躲过。
  - `Manager.Occupancy` 统一提供任务库快照；读库失败时记一条日志并只保留磁盘与排队项两类来源——命名不该因为一次读库失败就给不出任何建议。
  - `DestinationOccupied`（重复链接裁决用的「成品是否就在目标位置」）搬进同一模块并保留原语义：它只认磁盘事实，与命名占用不是同一个问题（排队项还没落盘）。`CheckURLFilesExist` 的跨目录复用语义按要求未动，只把它的建议名换成同一份判定。
  - `CONTEXT.md` 的「同名文件冲突」按新规则重写（来源、排除项、与裁决的关系）。
- **验证**（实际执行）：
  - `internal/window/queue_test.go` 新增用例：三个排队项先后登记同一目标名，建议名依次为 `report (1).pdf`、`report (2).pdf`，后两项报冲突，第一项不被自己的名字判为冲突。
  - `app_test.go` 新增用例：同一条规则在 App 绑定上的表现（正在编辑的那一项不算与自己的名字冲突；另一项占着的名字必须报冲突并给出未被占用的序号副本）。
  - `internal/engine/fileinfo_test.go`：三类来源各自生效、已完成且成品文件已不在磁盘上的记录不算占用、建议名本身必然未被占用、`Suggest` 与 `NumberedCopy` 的语义差异；原有用例改为读同一份判定。
  - `node scripts/quality-gate.mjs` 全绿（Go 全包、前端 88 项、扩展 79 项、8 项质量设施测试）。
- **影响面**：`internal/engine/occupancy.go`（新增）、`internal/engine/manager.go`、`internal/window/queue.go`、`app.go`、`CONTEXT.md`；Wails 绑定签名与载荷结构未变，前端无需改动。
- **风险**：冲突判定多了一类来源，界面上会出现「名字被另一个排队项占着」的冲突态；它走既有的冲突提示与「使用序号」路径，并会挡住提交直到用户选定动作——这是有意的（否则两笔下载会落到同一个文件）。已完成但成品已不在磁盘的历史记录不参与占用，与 `CleanMissingNumberedCopies` 的淘汰口径一致。

## 4. C4 给环回通信契约一个归属模块

- **强度**：强
- **状态**：已完成（2026-09-23），门禁通过
- **问题**：端点路径、鉴权头、事件名、端口与顺延步长、载荷结构、固定扩展 ID 被写了两遍且无归属；两边「契约测试」各自钉住自己那份字面量，扩展与桌面端可静默漂移。
- **证据**：`internal/server/server.go` 的 mux 注册、鉴权头与 CORS 列表、`MaxPortAutoIncrementSpan`，`internal/config/model.go:111` 的 `DefaultServerPort`，`internal/events/events.go` 与 `frontend/src/lib/events.ts` 的事件名，`extension/lib/client.ts` 的端口/顺延步长/路径/头/WebSocket 事件名，以及两端各自钉住字面量的测试。
- **实际改动**：
  - 新增 `internal/protocol`：环回端点路径、鉴权头、查询参数、默认端口与顺延步长、环回 WebSocket 事件名、界面事件名，以及扩展 ID 与 Origin。原先 `internal/events` 整包、`config.DefaultServerPort`、`server.PinnedExtensionID`/`EventServerMigrated`/`MaxPortAutoIncrementSpan`、前端 `lib/events.ts` 与 `lib/constants.ts` 全部删除，两侧只引用这里或它的镜像。
  - `scripts/protocol.mjs` 从 protocol.go 生成两份 TS 镜像（`extension/lib/protocol.generated.ts`、`frontend/src/lib/protocol.generated.ts`）；门禁新增 stage `protocol`，两侧不一致直接失败。生成器的输入契约写在 protocol.go 的包注释里（`// mirror 目标.分组` + 字面量/整数，或 `BasePath + "字面量"`），写法不合契约会报错，不会静默漏生成。
  - 扩展 ID 由 `extension/wxt.config.ts` 的固定公钥重新推导（SHA-256 前 16 字节按 a–p 编码）并与 Go 常量比对：换了公钥却忘了改常量，门禁直接失败——这正是 ADR-0005 决策 3 最容易漏的一步。
  - 扩展的 `DEFAULT_LOOPBACK_PORT`/`PORT_FALLBACK_SPAN`、前端的 `DEFAULT_SERVER_PORT` 与 `Event` 表、设置页里写死的「默认 9248」都改为读镜像。
  - 两侧「钉字面量」的断言删除（`client.test.ts` 两条静态断言、`server_test.go` 的空壳用例）：漂移保护移到生成器与门禁；跨进程契约的取值一侧由生成器产出、另一侧由门禁比对，不再各写一份。`docs/agents/post-implementation-checklist.md` 里「抽完常量再加断言测试钉住字面量」的旧做法一并改写。
  - 代码评审发现并修复（附一条回归测试）：`DesktopClient` 的请求地址一度由「带协议前缀的 baseUrl」与「已含前缀的路径」拼接，得到 `/api/v1/api/v1/...`；新增用例逐个断言六条请求的真实地址，修复前该用例失败。
- **未纳入镜像**：载荷结构（`internal/server/types.go` 与 `extension/lib/types.ts`）仍两侧各写一份。这是有意的边界：本项覆盖的是寻址事实（端点、头、端口、事件名、身份），它们的漂移会让扩展直接连不上；载荷字段的漂移由后端与扩展各自的行为测试守着，而把 Go 结构体生成成 TS 接口要引入第二套生成机制（反射 + 模板），代价与收益不成比例。若后续确有需要，作为独立项讨论。
- **验证**（实际执行）：
  - 漂移反证：把 protocol.go 的 `PathPing` 改成 `/ping2` 后，`node scripts/protocol.mjs --check` 与 `node scripts/quality-gate.mjs --stage protocol` 均失败并指出过期文件；还原后通过。
  - 公钥反证：把扩展公钥末尾改一位后 `--check` 以「ExtensionID 与公钥推导结果不一致」失败。
  - 质量设施新增用例：临时目录内复刻协议文件与生成器，验证「漂移必失败 → `--write` 后镜像跟着 Go 侧走 → 公钥与 ID 不符必失败」。
  - 扩展新增用例：六条环回请求的地址与鉴权头逐个断言（回归保护见上）。
  - 全量 `node scripts/quality-gate.mjs` 通过（Go 全包、前端 88 项、扩展 78 项、9 项质量设施测试）；扩展与前端仍各自独立构建（gate 的 `extensionBuild` 与 `frontendBuild` 阶段均通过）。
- **影响面**：`internal/protocol`（新增）、`scripts/protocol.mjs`（新增）、`scripts/quality-gate.mjs`、`internal/server`、`internal/config`、`app.go`、`internal/window`、`extension/lib` 与 `entrypoints`、`frontend/src`、`AGENTS.md`、`CONTEXT.md`、`docs/agents/quality.md`、`docs/agents/post-implementation-checklist.md`。
- **风险**：新增一个生成步骤（`node scripts/protocol.mjs --write`）；产物入库且门禁校验，遗忘执行只会让门禁报错，不会让产物悄悄过期。ADR-0006 定的通道与载荷格式未触碰。

## 5. C1 让下载入口只有一个归属模块（含删死代码）

- **强度**：强
- **状态**：已完成（2026-09-23），门禁通过
- **问题**：凭据/Cookie/Referer 整理、链接刷新、取消语义、提交生命周期在 `app.go` 三个处理器里各写一遍，又与 `QueueController` 形成只靠注释维持的口头协议；早期「提前下载」流程留下的绑定方法仍在发布面上（前端 0 调用）。
- **证据**：三个处理器里逐字重复的 headers 组装（交接、清晰度列表、媒体概览，交接那份还多一条 Referer 补齐）；`frontend/bindings/sheep-get/app.ts` 的 62 个绑定函数里，14 个在生产代码里零消费者——前端 0 调用，Go 侧也只有测试在用。
- **实际改动**：
  - 删掉 14 个死入口/死转发：`AddTask`（绕过队列直接建任务的第二条入口，自己又决定一次目录与并发）、`StartPreDownload`/`ConfirmPreDownload`/`CancelPreDownload`（提前下载的时机由队列决定）、`ResolveDuplicate`/`ReuseExistingFile`（重复裁决由队列在提交时调引擎）、`CheckURLConsistency`/`UpdateTaskURL`/`ResetAndDownloadWithNewURL`（旧链接刷新入口，界面从未有入口）、`SetLaunchAtStartup`/`IsLaunchAtStartup`（自启动走设置：UpdateSettings → OnSettingsUpdated → 同步系统注册），以及 `GetFileInfoQueueItems`；只读辅助 `GetDefaultDownloadDir`、`GetFileInfoQueueLength` 改为不导出（调用点都在本包内）。
  - 重新生成 Wails 绑定：62 → 48 个方法；`engine.ConsistencyResult` 随之退出前端模型图（只有被删的 `CheckURLConsistency` 引用过它）。剩下的 6 个「前端不调用」方法逐个核对都是 Go 侧接线：任务/设置监听回调，以及主程序的菜单与退出。
  - 扩展交过来的请求上下文只整理一次：新增 `handoverHeaders`（`app_loopback.go`），交接、清晰度列表与媒体概览三条路径共用；此前同样的 8 行在三个处理器里各写一遍。
  - 环回适配层整体移出 `app.go`：`app_loopback.go` 收下适配器、站点排除、交接、清晰度与媒体概览，`app.go` 只留绑定、设置与系统外壳适配。
  - `app_test.go` 里锚定已死流程的用例按新契约重写：任务生命周期与「取消提前下载保留暂停任务」改走「登记 → 提交/取消」的真实入口；文件信息窗口全流程改走队列（预下载起步 → 确认 → 重复检测 → 序号副本动作）；链接刷新的端到端用例删除——能力与用例都在引擎层（`TestManager_CheckURLConsistency`、`TestManager_UpdateTaskURLResumesWithRequestHeaders`），而界面没有入口；自启动用例改走设置这条真实路径。
  - 交接用例补上请求上下文断言（扩展给的自定义头、Cookie 与页面 Referer 都落到任务上），覆盖这次去重触及的行为。
- **验证**（实际执行）：
  - `go test -count=1 ./...` 全部通过；`node scripts/quality-gate.mjs` 全绿（Go 全包、前端 88 项、扩展 78 项、9 项质量设施测试）。
  - 绑定面复算：重新生成后 48 个方法，前端未消费的只剩 6 个 Go 接线方法（逐个核对来源：engine 监听接口 ×2、设置/剪贴板监听 ×1、main.go 菜单与退出 ×3）。
  - 全链路：登记 → 文件信息窗口 → 提交 → 取消由重写后的用例覆盖（含取消后保留暂停任务与 `.sheepget` 分片）。
- **未做**：没有再新起一个 `intake` 包。它只能是 `window.QueueController` 的包装：五个操作里 Enqueue / Submit / Cancel / SelectVariant 已经长在那个类型上，而交接的翻译必须留在环回适配层——把 loopback 的载荷类型下沉进窗口包，正是既有注释在挡的事（不让 server 包的类型泄漏进前端模型图）。再包一层只会多一个转发者，不增加归属。
- **影响面**：`app.go`、`app_loopback.go`（新增）、`app_test.go`、`frontend/bindings`（重新生成）。
- **风险**：删除的都是没有消费者的绑定，行为由队列、引擎与设置路径继续覆盖。Wails v3 beta 的 `ServiceOptions` 没有排除绑定的选项，Go 侧接线用的导出方法仍会出现在发布面上——这一项到此为止，不再为此引入包装层。

## 6. C3 窗口生命周期收成一个模块，每窗口一份声明

- **强度**：强
- **状态**：已完成（2026-09-23），门禁通过
- **问题**：三个窗口的创建 / 显示 / 定位 / 关闭钩子 / 闲置销毁分别写在三个方法里，另有每窗口一个 timer 字段与两处 `switch name`；无头环境里拿不到宿主对象，这段策略没有任何验证路径。
- **证据**：`wailsWindowView` 的 Show/Hide 里内联着文件信息窗口的声明与关闭钩子、`ensureMainWindow` 里内联着主窗口、`ShowProgressWindow` 里内联着进度窗口（含两处重复的停靠坐标计算）；`fileInfoTimer`/`progressTimer`/`destroyingWindows` 与 `cancelWindowIdleDestroy`/`scheduleWindowIdleDestroy` 的两处 `switch name` 承载同一条闲置销毁策略。
- **实际改动**：
  - 新增 `internal/windowing`：窗口声明（`Options`：名字、标题、地址、几何、边框、透明、禁用缩放、底色、初始位置）与生命周期策略（`Policy`：`OnClose`、`Busy`、`IdleDestroy`、`OnIdleDestroy`、`Position`）由注册表持有，宿主只以 `Host` / `Window` 两个接口出现。可注入假时钟与假窗口。
  - 新增 `app_windows.go`：唯一知道 Wails 窗口类型的地方——`windowsHost`/`wailsWindow` 适配器把声明翻译成 `WebviewWindowOptions`，三个窗口的声明与策略各写一份集中在这里；主题底色、主窗口关到托盘、文件信息窗口「关闭即取消当前项」、进度窗口停靠右下角与销毁后重新定位，都在声明里。
  - 闲置销毁收成一个泛化实现：每个窗口一个定时器（不再是每窗口一个字段），宽限期到点后仍不可见且不 Busy 才销毁；`Busy` 就是文件信息窗口的「队列非空不销毁」，`IdleDestroy` 就是轻量模式开关，销毁前跑一次 `OnIdleDestroy`（进度窗口据此重新定位）。打开轻量模式时立刻收掉不可见窗口也走同一条销毁路径。
  - `app.go` 不再直接碰 Wails 窗口：窗口字段与两处 `switch name` 全部删除，只剩面向界面/托盘的操作（显示、隐藏、最小化、贴顶、高度调整）转给注册表。
  - 新增用例：`internal/windowing` 覆盖宽限与取消、忙/可见不销毁、轻量模式开关、定位只做一次与重置后重新定位、无工作区时的兜底、创建地址只在创建时生效、关闭结论、退出时取消定时器；`app_test.go` 用记录型宿主驱动 **App 自己登记的声明**，验证主窗口「关到托盘 / 轻量模式放行」与「关掉文件信息窗口 = 取消当前项」。
  - 代码评审发现并已修（各附/调整一条用例）：`Declare` 只写声明表、运行时状态里另存过一份声明副本，导致「重新登记同一窗口」对已建状态的窗口不生效——副本删除，声明只留一份；`Options.AlwaysOnTop` 没有任何声明方消费（进度窗口的置顶是运行期状态），删除。
- **验证**（实际执行）：
  - `go test -count=1 ./...` 全部通过（含 11 项 `internal/windowing` 用例与 2 项 App 侧窗口策略用例）；`node scripts/quality-gate.mjs` 全绿。
  - 行为等价性：三个窗口的选项、关闭结论与端到端状态（拦截后窗口隐藏且存活、轻量模式放行、队列空才销毁）逐条对照原实现；差异只有一处——关闭钩子里「拦下」与「收尾动作」的先后（原实现先 `event.Cancel()` 再隐藏，现在收尾先跑、由适配器随后取消），终态相同。
- **影响面**：`internal/windowing`（新增）、`app_windows.go`（新增）、`app.go`、`appconsts.go`（颜色改为宿主中立的 `windowing.Colour`）、`app_test.go`。
- **风险**：Wails v3 Beta 的钩子取消语义在 macOS/Linux 未实测——这里没有假设它等价，只是把同一条 `event.Cancel()` 判断搬进注册表，平台侧仍由 Wails 决定；三平台的实际窗口行为需要在各平台验收时确认。

## 7. C7 为扩展 Service Worker 立一条「就绪」接缝

- **强度**：值得探索
- **状态**：已完成（2026-09-24），门禁通过，未提交
- **问题**：只要下载事件在未 await 的 `init()` 读完存储之前到达，接管判定就会对着后缀清单为空的默认配置做出；失败静默（下载留在浏览器完成）。
- **证据**（改动前）：`extension/entrypoints/background.ts` 里未 await 的 `init()` 与模块级 `currentConfig`、`extension/lib/storage.ts` 的空清单默认值、`extension/lib/handover.test.ts`（仅覆盖纯函数，`background.ts` 没有测试入口）。
- **实际改动**：
  - 新增 `extension/lib/takeoverConfig.ts`（接缝）：`readyConfig()` 读回本地缓存——整个 Service Worker 生命周期只读一次，读回之前不给出规则；`applyConfig()` 采用桌面端下发的规则并落盘。规则只能从这个出口取到，判定方拿不到「模块初值当规则」这种可能。
  - 只有持久化过的状态才有「就绪」可言，所以模块不收纳会话与媒体池：会话在消费点（`ensureDesktop` → `reverifyLink`）自己等存储，媒体池是易失的嗅探缓存、没有可加载的来源。`storage.ts` 与 `handover.ts` 经核对无需改动（评审影响面里的这两处不是缺口）。
  - `background.ts`：模块级 `currentConfig` 与 `init()` 里的即时赋值删除；下载判定、`reconcileTakeoverConfig`、WS 广播三条路径统一改走接缝。`reconcileTakeoverConfig` 先 `await readyConfig()` 再比版本，HEAD 版本比较用的是已恢复的值。
  - 读取期间落进来的桌面端广播不会被随后 resolve 的旧存储值覆盖（`hydrate` 里 `if (!hydrated)`）：读取与实时更新并发是接缝的固有形态，这条守住「模块里总是较新的那份规则」。
  - 删除 `chrome.storage.onChanged` 对 `sheepget_takeover_config` 的监听：扩展侧唯一写入方就是这个 SW 自己，监听只能收到自己的回声（`applyConfig` 已就地采用）。`STORAGE_KEYS` 随之改为导出，键名只在 `storage.ts` 出现一次。
  - `logDownloadDecision` 改为接收事实对象：规则不再从模块变量读，而是在判定时传进来（这也让它不必再依赖模块状态）。
  - 按住的快捷键不再随 Service Worker 挂起丢失：内容脚本的本地掩码是随页面存活的（它才是权威来源），因此新增一问一答 `QUERY_KEY_STATE` → `KeyStateReport`（`lib/types.ts` 按方向拆成 `BackgroundMessage` / `ExtensionMessage`）：**每次判定都现问一遍**（不缓存——缓存会在最需要它的时候过期），250ms 上限兜住主线程正忙的页面；页面只在真的按住键时应答（`lib/shortcuts.ts` 的 `keyStateReply`），所以「没人应答」就是「没按住」，不必为否定结论等超时。
  - 为什么是「每次问」而不是「没听到按键消息才问」：`SHORTCUT_CLICKED` 说的是**点击那一刻**的按键，3 秒后即过期，它不能证明「现在没按住键」。用它当作「已知按键状态」的判据，正好丢掉「按住键 → 点下载链接 → 下载几秒后才开始」这一种（判定时掩码为空，接管照样发生）。
- **验证**（实际执行）：
  - 新增 `extension/lib/takeoverConfig.test.ts`：读回之前不给规则、读回之后给出已恢复的规则且只读一次存储；读取期间落进来的规则不被旧值覆盖且已落盘；存储读不出来时就绪仍然成立（按空清单判定，不把判定挂住）。
  - 新增 `extension/lib/downloadIntercept.test.ts`：驱动真实入口与真实 `downloads.onCreated` 监听，用假浏览器复刻「唤醒这次 SW 的就是那个下载事件」——① 规则读取卡住时判定不许动这次下载（不 pause、不 resume），放开后按已恢复的 `zip` 清单接管；② 先投一条已过期的 `SHORTCUT_CLICKED`（复刻「按住 Delete → 点下载链接 → 下载几秒后才开始」）再触发下载：页面按住 `Delete` → 判定 `pause_shortcut_active`，下载留在浏览器；③ 页面按住 `Insert` → 判定 `force_shortcut_active` 强制接管，且第二次判定会再问一次页面。判定结论本身没有别的出口（不接管时它对下载什么都不做），断言读的是它留下的诊断日志。
  - 反证：把 `entrypoints/background.ts` 换回改动前的版本，① 以「等待超时：按已恢复的规则判定接管后，把下载还给离线的桌面端」失败（下载被静默留在浏览器）；把判定前的询问改成「收到过点击消息就不问」，② 以「预期不接管」失败（下载照样被接管，这正是「按住 Delete 没用」的那个现象）；删掉询问本身，②③ 都失败。三处换回后均通过。
  - `extension/lib/shortcuts.test.ts` 新增 `keyStateReply` 用例（按住才应答、没按住不应答）。
  - `bun test`（扩展 85 项）、`bun run compile`、`node scripts/quality-gate.mjs` 全绿。
- **影响面**：`extension/lib/takeoverConfig.ts`（新增）、`extension/lib/takeoverConfig.test.ts`（新增）、`extension/lib/downloadIntercept.test.ts`（新增）、`extension/entrypoints/background.ts`、`extension/entrypoints/content.ts`、`extension/lib/storage.ts`、`extension/lib/shortcuts.ts`、`extension/lib/types.ts`。
- **行为变化**：冷启动窗口里的下载判定从「按空清单不接管」变为「等规则就绪后按真实清单判定」；按住的暂停键与强制键在 SW 被挂起重建后仍然生效（每次判定都现问一次页面）；配置的存储变化不再被监听（无其它写入方）。其余为等价收敛。
- **风险**：Chromium 挂起阈值不可控，这是概率性竞态——本项以「不变式在模块内成立」为准，不以复现率证明。就绪与按键询问都未进入环回契约：popup 与 SW 的消息、桌面端下发的载荷都没有变化。按键状态仍以「页面还能回答」为前提：没有内容脚本的页面（特权页）与内容脚本被卸载的标签页本就上报不了按键，这里不改变它们的语义。
- **未实测**：本轮没能跑起真机端到端——本机 Chrome 153 已不接受 `--load-extension`（137 起移除），用 CDP `Extensions.loadUnpacked` 加载的扩展在自动化连接里既不出现在 `chrome://extensions-internals`、也观察不到它的 Service Worker target。因此 `chrome.tabs.query` / `tabs.sendMessage` 这条询问链路目前只由假浏览器用例覆盖，真机确认需要在 Chrome 里加载重建后的扩展、按住 `Delete` 点一次下载，看 SW 控制台里 `[SheepGet] download decision` 的 `reason` 是否为 `pause_shortcut_active`。
- **已知边界**（用户确认保持现状）：悬浮条 / 媒体面板的交接是一次显式点击，不经接管规则（含暂停/强制键），也不写 `download decision` 日志——`handleMediaHandover` 只整理 Cookie/Referer 后投递。按住 `Delete` 点悬浮条仍会投递；要拦住它需要单独的产品决定（拦住后这次下载落在哪里）。
- **遗留发现**（未做）：按键的「松手宽限」仍是 3 秒（`SHORTCUT_GRACE_PERIOD_MS`），下载若在松手 3 秒后才开始，那次快捷键意图会失效。这与按键是否按住无关（按住由本项的询问覆盖），属于既有产品规则，调整需单独讨论。

## 8. C6 把目标落点记在任务上，不让界面重推一遍

- **强度**：值得探索
- **状态**：已完成（2026-09-24），用户选定方案 A（历史归属语义），门禁通过
- **问题**：CONTEXT.md 写明目标落点由后端判定一次、界面读取结果，但 `task.Task` 上没有命中分类字段，主列表对每个任务、每次渲染都要跑一遍 TypeScript 版的优先级规则。
- **证据**：`frontend/src/lib/category.ts:51-99`、`frontend/src/lib/taskFilter.ts:18`、`frontend/src/App.tsx:201`、`frontend/src/category.test.ts:23-134`、`internal/task/task.go:41-72`、`internal/config/model.go:544-596`。
- **实际改动**（按方案 A 历史归属语义落定单一事实来源）：
  - `internal/task/task.go`：在 `Task` 结构体新增 `CategoryID string`（JSON: `categoryId`），`Task.Clone()` 与持久化存储自动带出该字段；
  - `internal/engine/manager.go`：新增 `SetTaskCategoryID(ctx, taskID, categoryID)` 提供分类更新与持久化；
  - `internal/window/queue.go`：`FileInfoSubmission` 增加 `CategoryID`；预下载启动时赋给任务，提交（`Submit`）时优先采用用户在弹窗手动指定的分类、否则沿用登记时自动命中的分类，建任务后直接固化；
  - `app.go`：在应用启动生命周期（`App.startup`）对旧版本留下的无分类历史任务统一补齐并落盘固化，`ListTasks` 严格保持纯只读（符合读写分离 CQS）；
  - 重新生成 Wails TS 绑定（`frontend/bindings`），类型严格对齐；
  - `frontend/src/lib/taskFilter.ts` 与 `frontend/src/App.tsx`：主列表筛选直接比对 `t.categoryId === selectedCategory`，彻底移除对 `settings` 的依赖；
  - `frontend/src/lib/category.ts` & `frontend/src/category.test.ts`：彻底删除前端重复维护的 `matchTaskCategory` 及其 112 行冗余测试。
- **验证**：
  - `internal/task`、`internal/window` 与 `app_test.go` 新增测试覆盖分类持久化、提交覆盖与启动历史数据迁移；
  - 前端 85 项测试与类型检查全部通过；全量质量门禁 `node scripts/quality-gate.mjs` 全绿。

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
