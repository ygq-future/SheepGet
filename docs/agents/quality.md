# SheepGet 质量契约与门禁状态 (Quality Contract)

本文档依照项目 `project-quality` 规范与 `quality-contract.md` 建立，记录当前全栈质量门禁的真实覆盖范围、执行工具与有效性基准。

## 统一执行入口

- **默认完整质量门禁**：
  ```bash
  node scripts/quality-gate.mjs
  # 或
  bun run quality
  ```
- **退出契约**：严格遵循 Check-Only 原则，任一检查项失败即非 0 退出阻断流程，无隐蔽修补、不篡改源码与锁文件。

---

## 全栈能力覆盖矩阵 (Coverage Matrix)

| 能力 (Capability) | 负责工具 / 配置 | 执行命令 | 覆盖范围 / 目标 | 执行阶段 / 严重性 | 状态与客观证据 (Status & Evidence) |
|---|---|---|---|---|---|
| **代码格式检查 (Formatting)** | `gofmt` (Go) + `prettier` (Web) | `gofmt -l .` & `prettier --check .` | 全量 Go 源码、前端 TS/TSX/CSS/JSON | Pre-commit (本地阻断) / Error | **已验证 (Verified)**：`gofmt -l` 严格 check-only，Prettier 配套 `prettier-plugin-tailwindcss` 检查类名排序通过。 |
| **语法与编译 (Syntax / Compile)** | `go build` / `vite build` | `go build .` / `bun run --cwd frontend build` | 后端主模块、前端生产打包目标 | Pre-commit / CI / Error | **已验证 (Verified)**：Go 1.27 与 Wails/Vite 生产构建均能正常生成产物。 |
| **类型检查 (Type check)** | `tsc` (TypeScript 5.9) | `bun run --cwd frontend typecheck` | 前端 `frontend/src/**/*.ts(x)` | Pre-commit / CI / Error | **已验证 (Verified)**：`tsc --noEmit` 严格模式无报错。 |
| **代码规范与 Lint (Lint)** | ESLint 10 + typescript-eslint 8 | `bun run --cwd frontend lint` | 前端源码 (`eslint.config.js`) | Pre-commit / CI / Error | **已验证 (Verified)**：采用 ESLint 官方 `defineConfig`，已消除废弃警告，0 errors, 0 warnings。 |
| **编译器与静态分析 (Static Analysis)** | `go vet` | `go vet .` | Go 后端核心模块 | Pre-commit / CI / Error | **已验证 (Verified)**：检查可疑结构、并发错误与格式化占位符。由于 Go 1.27.1 过新，外部 `golangci-lint` 暂未适配 Go 1.27 AST export 数据版本，当前以 `go vet` 官方原生分析为主。 |
| **测试与验证 (Tests)** | `go test` (Go) | `go test .` | 后端单元测试 | Pre-commit / CI / Error | **脚手架基础设施已就绪，业务测试待补齐 (Bootstrap verified; tests pending)**：当前为初始脚手架，无存量业务逻辑。按契约：后续在 Ticket 01 首次编写下载核心业务行为时，必须同步建立真实端到端测试用例。 |
| **依赖与清单一致性 (Dependency Manifests)** | `go mod verify` & `bun install` | `go mod verify` | `go.mod`, `go.sum`, `bun.lock` | 门禁阶段 / Error | **已验证 (Verified)**：`go mod verify` 校验通过，模块哈希一致。 |
| **提交格式验证 (Commit Message)** | `@commitlint/cli` + conventional | `bun x commitlint --edit "$1"` | Git 提交历史 | Commit-msg 钩子 / Error | **已验证 (Verified)**：强制拦截非规范 commit message（实测非法格式阻断，合法格式放行）。 |
| **本地提交拦截 (Git Hooks)** | Git 原生 Hooks | `.git/hooks/pre-commit` & `.git/hooks/commit-msg` | 本地开发者工作流 | Pre-commit / Commit-msg | **已验证 (Verified)**：执行本地门禁全检与提交信息拦截。 |
| **跨平台持续集成 (CI Quality Gate)** | 待远端仓库配置 | 共享 `node scripts/quality-gate.mjs` | Windows, macOS, Linux | GitHub Actions (待配置) | **待配置 (Pending)**：本地门禁就绪，等待远端仓库推流后配置 CI 工作流。 |

---

## 当前诊断限制与边界说明 (Diagnostic Limitations)

1. **Go 1.27.1 工具链兼容边界**：
   - 现状：由于使用了最新的 Go 1.27.1，社区静态分析聚合器 `golangci-lint`（最新 `v1.64.8`）其内部引用的 `goarch` 解析器尚不支持 Go 1.27 的 AST export data version 4。
   - 对策：在当前阶段，后端静态分析依托 Go 官方随编译器发布的 `go vet` 与 `go mod verify`，保证工具链运行的 100% 稳定性；待 `golangci-lint` 官方发布对 Go 1.27 的支持版本后再平滑升级挂载。
2. **测试待落地状态说明**：
   - 当前项目无业务代码，`go test` 报告 `[no test files]`。依据质量契约规范：**基础设施已验证，测试用例随首期 Ticket 01 下载核心行为落地，严禁编写仅为应付门禁的虚假无意义测试**。
