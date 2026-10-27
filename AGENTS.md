# SheepGet 项目约束

## 项目状态与依据

- 项目已完成工程脚手架初始化并配置质量门禁，且已交付 Ticket 01（核心下载引擎与主界面）、01A（Wails v3 架构迁移）、02（独立文件信息窗口与重复链接策略闭环）、02A（配置中心）、03（共享下载进度窗口）、04（文件与分类）、05（常规与下载行为）、06（浏览器扩展与媒体入口）、07（HLS 媒体下载完整流程）以及 Ticket 08（跨平台安装包与整体验收）的完整实现与闭环验证。
- 项目第一版产品规划中所有实施阶段均已全部落地并完成质量门禁校验。
- 当前目标为 Windows、macOS、Linux 桌面下载管理器，技术方向为 Wails v3 Beta (`v3.0.0-beta.20`) + Go (1.27) + Web 前端，配套浏览器为 Chrome 和 Edge。
- 产品范围与验收以 GitHub 母 issue #1 为依据；任务范围与状态以对应实施 issue（#2 ～#11）为准。
- 探索领域前读取 `CONTEXT.md` 和相关 `docs/adr/`。术语表仅记录领域语言，架构决策写入 ADR，功能与验收直接在 GitHub Issues 闭环跟踪。
- 技术选型已确认：
  - 桌面框架：Wails v3 Beta (`v3.0.0-beta.20`)（遵循 ADR-0002）
  - 后端：Go 1.27 (`go 1.27.1`)
  - 前端基座：React 19 + TypeScript + Vite
  - 样式与组件：Tailwind CSS v4 + Radix UI 原语 (`@radix-ui/react-*`) + Lucide 图标
  - 交互与过渡动画：`motion` (Framer Motion)
  - 纯 UI 临时状态：`zustand`（保持 Go 后端为任务状态唯一事实来源）
  - 提交规范与校验：Conventional Commits (`@commitlint/cli` + `@commitlint/config-conventional` + `.githooks/commit-msg`)
  - 包管理器与运行时：Bun (`v1.4.2`)，Node.js (`v24.21.0`)
  - 媒体处理：严格遵循 ADR-0001 原生 Go 媒体处理层
  - 代码检索：`XERJ prefix: "sg"`

## UI 与交互约束

- 整体采用 **Modern refined desktop utility UI**，参考 Linear + Raycast 的克制、精致、高信息密度。下载列表是主界面核心，常用操作易发现，高级选项收于次级入口。
- 布局、字体、间距、颜色、状态反馈和组件行为应统一。组件交互与状态变化提供流畅过渡；动画不得阻塞操作或干扰进度信息阅读。
- 避免用大面积装饰、卡片堆叠、过量留白或复杂 Dashboard 取代任务信息。UI 展示用户需要的信息，不暴露无助于决策的内部实现细节。
- 文件信息窗口始终保留确认、名称和保存位置编辑能力；提前下载开关只改变后台传输时机。
- 清晰度在进入文件信息窗口前选定，进入后固定；悬浮条和手动 m3u8 入口遵循相同顺序。
- 多任务共用一个独立下载进度窗口；窗口限高并内部滚动，下载中按进度降序实时排列，完成信息固定顶部。完成后的打开文件、打开文件夹等操作保持稳定可用。
- 完成信息的关闭只移除该展示区域，保留主列表记录；窗口显示和完成信息设置按规格独立生效。
- 媒体悬浮条小巧、停靠在对应播放器右上角外侧（右边缘与播放器对齐、不遮挡画面；上方没有位置时落到播放器下方），随播放器和视口变化；取消文件信息窗口后仍可用，手动关闭仅影响对应播放器直到刷新或重开。
- 关闭主窗口固定驻留托盘；重启软件后由用户手动继续未完成任务。
- 等待、未知大小、暂停、下载失败、处理失败和完成应有准确可辨的反馈，不用伪造数值或成功提示掩盖未知状态。

## 架构与业务边界

### 状态与下载核心

- Go 后端是任务生命周期、队列、进度、持久化和业务规则的单一事实来源。前端负责展示及提交用户操作，不另建独立业务状态机。
- HTTP Transfer Engine 与 HLS Engine 各自处理适合的传输逻辑，通过任务编排边界接入；不要将 UI 组件或浏览器扩展耦合到具体下载实现。
- HTTP 使用可配置的固定并发上限和动态分块。慢区块优化仅在既定上限内有限再拆分剩余区间，保证写入互斥与已下载数据正确。
- 暂停、重试、续传与更新链接必须保持文件版本一致性；不能仅因大小或文件名相同就假定可以拼接。
- 下载名额与媒体处理分离，传输结束立即释放下载名额。处理失败保留分片，重试处理复用已有分片。
- 分类规则决定保存目录，其文件类型全集作为下载接管与剪贴板匹配的唯一来源；接管策略独立维护排除站点与快捷键；自定义分类按用户排序优先匹配，保留内置后缀规则。
- 全局设置与单任务覆盖具有明确优先级，代理变更和提前下载等行为遵守规格，不隐式改变进行中的任务。

### 媒体处理

- 严格遵循 `docs/adr/0001-native-go-media-processing.md`：**HLS Engine → Media Processor → Output**。
- HLS Engine 负责 playlist、segment、加密、下载和媒体信息组织；Media Processor 负责 TS/fMP4 解析、音视频轨合并、demux、mux、remux 和输出。
- 默认 **Native Go Backend**，优先 Go 原生实现或成熟 Go 媒体库。具体库封装在后端内部，不直接进入 HLS Engine、任务系统或前端契约。
- 第一版不将 FFmpeg 作为必需依赖，也不默认打包完整 FFmpeg runtime。保持可替换后端边界，以便未来可选 FFmpeg Backend 接入，不提前实现其安装与更新系统。
- 优先覆盖常见点播 HLS、TS、fMP4 和 H.264/H.265 + AAC 组合，以正确组装与无损封装为目标，不做重新编码。
- 暂未支持的 codec、封装或复杂结构明确提示不支持。不能为少量兼容场景静默扩大核心范围，也不能用更改后缀冒充正确封装。

### 浏览器与跨平台

- Chrome / Edge 扩展负责发现资源、接管规则及向桌面端传递必要请求上下文；大文件下载和媒体处理由桌面端承担。
- 环回通道的线上事实（端点路径、鉴权头、查询参数、默认端口与顺延步长、事件名、扩展 ID）只在 `internal/protocol` 定义一次，TypeScript 镜像由 `scripts/protocol.mjs` 生成并由门禁 stage `protocol` 校验；不得在扩展或前端再写一份字面量。
- 接管确认与用户最终确认是不同阶段，正确处理断连、取消和失败回退，不能让浏览器与桌面端都丢失下载。
- 系统能力通过清晰的适配边界提供；验证托盘、剪贴板、启动、文件打开及浏览器通信的真实平台差异，不将能编译等同能运行。
- URL 中的令牌、Cookie、认证 Header 等按敏感请求上下文处理，不写入普通日志或无关界面。
- 首期不扩展至 Safari、系统级拦截其他应用下载、复杂站点专项解析、直播录制、DRM、重新编码或任意媒体格式兼容。

### 版本协同与发版核对红线（全链路版本一致性）

版本一致性由 `scripts/version.mjs` 统一判定，不再依赖人工清单：

1. **唯一事实来源（SSOT）**：`build/config.yml` (`info.version`)。版本号格式固定为 `X.Y.Z` 三段数字——Windows PE 版本资源、MSI `ProductVersion` 与 Chrome manifest 都只接受该形式。`scripts/package.mjs` 与版本模块共用同一套 `info` 块解析，并向下注入 Windows PE Version Info (`.syso`)、NSIS (`INFO_PRODUCTVERSION`) 以及 WiX MSI (`ProductVersion`)，不存在第二份版本读取实现。
2. **双轨发布与镜像唯一定义**：
   - **桌面端**：`scripts/version.mjs` 的 `MIRRORS` 表是全部镜像的唯一定义，当前覆盖根 `package.json`、`build/windows/info.json`（`file_version` 与 `ProductVersion`）、`internal/version/version.go`、`.github/workflows/release.yml` 的发布 tag、`README.md` 的安装包命名示例；
   - **浏览器扩展端**：浏览器扩展已实现解耦，版本完全由 `extension/package.json` 独立维护，发布由 `ext-v*` tag 驱动独立工作流（`release-extension.yml`）；
   - **发版前强制范围审计（Pre-release Scope Audit）**：在执行发版前，必须先执行 `git diff <last-release-tag>..HEAD` 审计变更集分布。若涉及桌面端代码，按桌面端发版流程演进；若涉及 `extension/` 源码或依赖，必须明确升级 `extension/package.json` 并发布对应扩展版本（`ext-v*`）；若双端均有改动，必须向用户明确汇报双端发版计划，严禁默认为仅发桌面端单轨；
   - **扩展版本防漂移硬约束**：凡自上一个扩展发布 tag（或解耦基线）以来 `extension/` 存在代码变更，`extension/package.json` 必须完成版本号自增；质量门禁 stage `version` 会自动对比 Git 历史进行强校验，未自增版本将直接阻断门禁与发版。
3. **升级与核验**：
   - 桌面端升级：`node scripts/version.mjs --set X.Y.Z` 一次性改写 SSOT 与全部镜像，改写后自动复核；
   - 扩展端升级：`node scripts/version.mjs --set-ext X.Y.Z` 改写 `extension/package.json` 并复核；
   - 全链路核验：`node scripts/version.mjs --check` 作为质量门禁阶段（stage `version`），任一处桌面端镜像漂移、声明改名或扩展版本漂移都会直接阻断失败；
   - 禁止手工逐处修改版本号，禁止在其他文档里维护第二份同步清单。
4. **发版验收硬指标（人工，Windows）**：
   - 发布前必须在 Windows 下检查生成的 `SheepGet.exe`“属性 -> 详细信息”中的“产品版本”与“文件版本”，确认已与目标版本号严格一致，严禁残留模板占位符；
   - 开启自启动后验证 Windows“设置 -> 启动应用”管理面板中的产品名称（`SheepGet`）与发布者（`SheepGet`）显示正常，严禁出现未解析模板宏。
5. **发布说明硬约束（Release Notes Mandatory）**：
   - 每次桌面端版本升级与发版，**必须**在 `.github/release-note/vX.Y.Z.md` 撰写本次版本的发布说明（包含核心特性、关键修复、各平台分发包表格与升级说明）；
   - 严禁缺失 Release Notes，严禁发布说明回退/静默使用旧版本模板；
   - `scripts/version.mjs --check` 与质量门禁（stage `version`）强制校验 `.github/release-note/vX.Y.Z.md` 存在且非空，未撰写将直接阻断门禁。

## 测试与质量门禁

- **业务测试验证**：
  - HTTP/HLS 使用可控服务和固定样本验证资源变化、失败、恢复及输出一致性；慢区块测试区分局部慢速与整体限速，不承诺未经测量的提速；
  - Media Processor 使用固定媒体样本验证轨道、时间戳、内容完整性及失败重试，并用独立检查避免仅凭同一库自读成功判定正确；
  - 浏览器交接和系统集成需要真实端到端验证；UI 检查共享窗口、悬浮条、排序、滚动、动画与失败状态。
- **全链路质量门禁**：
  - 统一入口为 `node scripts/quality-gate.mjs`（或 `bun run quality`）；
  - 首次克隆使用 `node scripts/bootstrap.mjs` 安装固定工具和本仓库 hooks；门禁本身采用 check-only，不能代替准备操作；
  - 开发与调试期间，优先按修改范围运行局部测试（如 `go test ./internal/...`）或单阶段门禁（`node scripts/quality-gate.mjs --stage <name>`）；全量 `node scripts/quality-gate.mjs` 仅在代码与测试全部就绪后执行一次，避免在同一轮任务中重复多次运行全量门禁；
  - 修改代码后执行适当检查；提交前在最终代码树运行完整门禁，修复本次引入的诊断，包括警告与弃用 API；
  - Go 与前端业务测试已随下载行为落地并纳入门禁：`scripts/test-scope.json` 的 scaffoldOnly 为 false，任一侧缺失测试即失败；
  - 本地提交校验与跨平台 CI 分别报告。macOS/Linux、远端 workflow 和分支保护未经实际验证不能称为已通过。

## Issue 跟踪与分诊标签

- **规格与任务**：以 `ygq-future/SheepGet` 的 GitHub Issues 为准。使用 `gh` CLI 操作时显式指定 `--repo ygq-future/SheepGet`，避免依赖本地 remote。
- **分诊标签**：
  | 标签              | 含义                        |
  | ----------------- | --------------------------- |
  | `needs-triage`    | 等待维护者评估              |
  | `needs-info`      | 等待补充信息                |
  | `ready-for-agent` | 规格充分，可交给 agent 执行 |
  | `ready-for-human` | 需要人工实现                |
  | `wontfix`         | 不予实施                    |
- **领域与架构决策**：采用根目录 `CONTEXT.md`（领域术语）和 `docs/adr/`（架构决策记录）的单一上下文布局。涉及领域术语与既有 ADR 时，以此二者为准。
