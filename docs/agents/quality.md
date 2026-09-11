# SheepGet 质量契约

## 当前状态与入口

当前是 Wails Greet 脚手架，下载业务尚未实现。质量基础设施与业务验收分别报告；无业务测试不是“所有测试通过”。

默认入口：`node scripts/quality-gate.mjs`，等价于 `bun run quality`。阶段定义以该脚本为唯一来源。

- `--stage NAME`：明确的局部检查，只用于定位，不能替代默认门禁。
- `--staged`：默认完整门禁之前要求索引与工作树一致；有未暂存改动或未跟踪文件则拒绝，不自动暂存或覆盖文件。
- `--message FILE`：校验实际提交信息文件。
- `--range BASE HEAD`：校验范围内所有提交；BASE 为 ROOT 时包含初始提交。遵循 commitlint conventional 默认 merge/revert 忽略语义。
- `--ci`：本地完整验证入口，默认门禁加 Wails 本机发布构建。GitHub Actions 将默认门禁与正式打包拆分为有依赖关系的 job；各系统分别执行，构建成功不等同于运行验收通过。

## 前置与安装

工具版本的唯一清单为 `scripts/quality-tools.json`；Node 24.21.0、Bun 1.4.0、Go 1.27.1。Go 模块锁定 Wails v2.15.0；根目录和前端分别拥有 Bun 锁文件。

首次克隆执行 `node scripts/bootstrap.mjs`：冻结安装两套 JS 依赖，将固定版本 golangci-lint、Wails CLI、actionlint 安装到项目 `.tools`，并仅设置本仓库 hooks 路径。CI 使用 `--ci` 不安装本地 hooks。bootstrap 是有副作用的准备操作，不属于 check-only 门禁。

本地 Go 分析使用 golangci-lint 2.13.2，负责 govet、Staticcheck、unused、errcheck、ineffassign。升级工具时必须重新验证规则与 Go 兼容性。不存在“最新工具永远不兼容”的假设。

## 能力矩阵

| 能力                   | 提供者及任务                                                                      | 范围                                               | 阶段 / 严重性   | 状态与证据                                             |
| ---------------------- | --------------------------------------------------------------------------------- | -------------------------------------------------- | --------------- | ------------------------------------------------------ |
| 格式                   | gofmt；Prettier 3 与 Tailwind 插件                                                | 维护的 Go、前端、根脚本/配置、工作流和质量相关文档 | 默认 / 失败阻断 | 已配置；非空 gofmt 输出即失败，Tailwind 类排序有正反例 |
| 语法与类型             | TypeScript 严格检查；Go build/分析                                                | 前端 src、Vite 配置、全部维护的 Go 包              | 默认 / error    | 已配置；不只检查根包                                   |
| Lint                   | ESLint recommended、typed recommended、React Hooks                                | 前端 TS/TSX、JS 配置；根脚本 JS 基础规则           | 默认 / 零警告   | 已配置；Hooks、any、Promise 反例已验证                 |
| 弃用 API               | TS no-deprecated；Staticcheck SA1019                                              | 有类型和弃用标记的 TS、Go                          | 默认 / error    | 正反例验证；不是检测工具配置的弃用来代替 API 检测      |
| 静态分析               | golangci-lint 明确规则集；类型感知 ESLint                                         | 维护的源码                                         | 默认 / error    | 已配置并验证新版本在本机工作                           |
| 未使用/不可达/可疑代码 | unused、ineffassign、Staticcheck、ESLint、TS unreachable                          | 同上                                               | 默认 / error    | 明确责任；不宣称覆盖所有 IDE 提示                      |
| 测试                   | Go test；Vitest；Node test                                                        | Go 包、前端 src 测试、质量设施测试                 | 默认 / failure  | 基础设施测试存在；Go/前端业务测试待 Ticket 01          |
| 构建                   | Vite、Go build；Wails build                                                       | 本机开发构建；三平台本机构建矩阵                   | 默认 / CI       | 默认不修改维护文件；跨平台结果独立记录                 |
| 依赖与配置             | go mod verify、tidy -diff；Bun frozen dry-run；golangci config verify、actionlint | 两套锁文件、Go 模块、分析配置、CI 工作流           | 默认 / error    | 冻结锁冲突已验证会失败；verify 仅证明缓存完整性        |
| 提交信息               | 根依赖中固定的 commitlint                                                         | 实际消息或整个提交范围                             | commit-msg / CI | 合法和非法输入已验证；不经 bun x 动态取包              |
| Git hooks              | 入库的 .githooks + bootstrap                                                      | 当前仓库                                           | 本地            | 已配置；克隆后必须执行 bootstrap                       |
| CI                     | GitHub Actions 三平台矩阵与 required 汇总                                         | Windows、macOS、Ubuntu                             | 合并/发布前     | 工作流已配置，远端执行与分支保护待验证                 |

## 检查范围与生成输入

- Go 包由实际模块枚举，排除 node_modules 中属于第三方依赖的 Go 示例；这些源码由锁文件归属依赖方，不是本项目维护源码。
- 前端生成的 Wails bindings 不交给格式/lint 修改，其声明由消费者 TypeScript 检查；构建使用已有绑定。变更绑定方法时应单独生成、审阅后再执行门禁。
- 前端生产构建先于 Go 分析/构建，保证 embed 的 frontend/dist 存在。Wails 发布构建在 .tools 下隔离副本运行，允许框架在副本内更新 runtime，再将成品复制到 build/bin；不会改动工作区 bindings。声明产物仅写入 frontend/dist、build/bin 与工具缓存。
- 根目录 Prettier 配置是公共格式选项来源，前端附加 Tailwind v4 stylesheet 和实际使用的 cn helper。
- 引擎/框架构建配置经实际构建验证，工作流经 actionlint 验证；actionlint 的可选 shellcheck/pyflakes 检查未启用，不宣称覆盖嵌入脚本的所有诊断。
- 当前仅一个 Go 模块和一个前端；增加模块、扩展源码或生成入口时必须同步纳入门禁，不能依赖空 glob 或忽略缺失任务获得成功。

## 失败契约与诊断限制

门禁通过直接进程调用传播失败，Node 子工具启用弃用异常，ESLint 零警告。门禁不自动修复、改锁文件、安装 hooks 或升级依赖；执行前后检查维护文件内容变化，发现变化则失败。依赖验证可能使用工具缓存或网络，前置依赖必须已安装。

React Hooks 和 TypeScript 语义规则已覆盖；JS 配置只做基础 ESLint，第三方缺失弃用标记、IDE 特有建议和完整无障碍规则仍有边界。当前 eslint-plugin-jsx-a11y 的 peer 范围止于 ESLint 9，与本项目 ESLint 10 不匹配，因此未强装；无障碍交互仍须通过组件/实际验收检查，后续选择兼容的检查能力。

只接受有证据的历史问题归因。当前没有允许忽略历史诊断的基线；真实误报或兼容例外须有精确诊断、范围、理由、负责人及复核条件，并经用户确认。不得关闭规则、删除有效测试或使用 --no-verify 绕过失败。

## 脚手架测试边界

`scripts/test-scope.json` 中 scaffoldOnly 当前为 true，仅用于这次空业务脚手架阶段。发现测试后会实际执行；无测试时打印 PARTIAL，而非伪造成功。第一项下载行为实施时必须设为 false，并补充 Go 与前端真实行为测试；false 时缺测试直接失败。该豁免不允许延伸至已有业务代码。

质量设施测试验证格式类排序、类型错误、Hooks、Promise、弃用引用、缺失工具、失败退出传播和提交信息校验；Go 反例仅在临时隔离目录运行，清理后再次验证通过。

## 本地与 CI 责任

本地提交前必须通过默认全部阶段；只有用户明确允许才可提交。pre-commit 要求全部拟提交内容与工作树一致，避免部分暂存导致校验与提交不一致。commit-msg 校验真实消息，两者均调用统一接口。

CI 使用固定 action 提交、固定工具版本和冻结安装，按以下依赖关系执行：

1. quality 三平台矩阵执行默认门禁（包含 Vite/Go 验证性构建）和整个提交范围校验。
2. build 三平台矩阵通过 needs: quality 等待全部质量检查成功，随后重新安装冻结依赖、构建前端并执行 Wails 正式打包。任一平台质量检查失败、取消或跳过，所有正式打包均跳过。
3. 每个平台仅在本平台打包成功后归档 build/bin 并上传至该次 Actions 运行的 Artifacts。归档使用 tar.gz 保留可执行权限与应用包结构，名称包含系统和架构，保留 14 天；缺少上传文件直接失败。产物是应用构建结果，不包含签名、公证或 GitHub Release 发布。
4. required 汇总 quality 与 build；只有两组矩阵全部成功（包括上传）才成功。它必须被仓库分支保护设为必需检查才能阻止合并。

Ubuntu 使用 GTK3/WebKitGTK 4.1 和 webkit2_41 标签。远端 workflow 首次执行、实际上传和保护规则仍需用户授权提交/推送后验证。

## 验证记录

本次在 Windows amd64、Node 24.21.0、Bun 1.4.0、Go 1.27.1 下验证：

- `node scripts/bootstrap.mjs` 成功，项目本地工具就绪，core.hooksPath 指向 .githooks。
- `node scripts/quality-gate.mjs --ci` 成功，包含全部默认阶段、7 项质量设施测试及 Windows Wails 发布构建；维护文件内容检查通过。
- `git hook run commit-msg` 对合法消息返回 0，对非法消息返回 1；未创建提交。
- `git hook run pre-commit` 在当前含未暂存变更时返回 1，符合索引一致性约束；没有为测试而暂存用户文件。
- actionlint 验证工作流语法和表达式通过；冻结锁文件与清单不一致的反例返回非零。
- Go 与前端业务测试仍为 PARTIAL，待首个下载行为实施；质量设施自测不是业务测试替代品。

macOS/Linux 运行结果及远端强制执行均未观察到，不能标为已通过。未建立历史问题忽略基线。
