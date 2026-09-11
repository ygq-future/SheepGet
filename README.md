# SheepGet

面向 Windows、macOS 和 Linux 的轻量桌面下载管理器。

当前仓库为 **Wails Greet 脚手架与质量基础设施**，尚未实现下载业务。计划能力包括 Chrome/Edge 下载接管、可恢复的 HTTP 分块下载、共享进度窗口和 Native Go 点播媒体处理；完整范围见 [产品规格](docs/spec.md)。

## 技术方向

Wails v2.15.0、Go 1.27.1、React 19、TypeScript、Vite、Tailwind CSS v4、Radix UI、Motion 和 Zustand。Go 后端是业务状态唯一事实来源。媒体处理遵循 [ADR-0001](docs/adr/0001-native-go-media-processing.md)。

## 开发准备

安装 Node 24.21.0、Bun 1.4.0 和 Go 1.27.1。系统还需具备 Wails 的平台前置环境；三平台兼容性通过各自运行验证，不能只看构建成功。

克隆后在根目录运行：

```sh
node scripts/bootstrap.mjs
```

该步骤冻结安装根目录及前端依赖，将固定版本工具安装到 .tools，并启用本仓库 .githooks。不会改全局 Git 配置。

开发启动使用项目本地 Wails：

```sh
# Windows PowerShell
.\.tools\wails.exe dev
# macOS / Linux
./.tools/wails dev
```

生产构建使用同一路径的 Wails CLI 执行 build。平台依赖和 Linux 标签见 [质量契约](docs/agents/quality.md)。

## 质量检查

```sh
node scripts/quality-gate.mjs
```

默认检查格式、依赖一致性、类型、lint/静态分析、测试和本机构建；检查不会自动修复维护文件。业务测试尚待实现时会明确打印 PARTIAL。质量设施自测可单独运行：

```sh
bun run quality:verify
```

GitHub Actions 已配置三平台检查，远端运行和分支保护尚待验证。门禁通过后仍需用户验收与明确提交许可。

## 文档

- [项目约束](AGENTS.md)
- [产品规格](docs/spec.md)
- [质量契约及限制](docs/agents/quality.md)
- [领域术语](CONTEXT.md)

## 协议

[MIT License](LICENSE)
