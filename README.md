# SheepGet

<p align="center">
  <b>一款轻量、克制、专注于下载本身的现代跨平台桌面下载器。</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square&logo=go" alt="Go Version" />
  <img src="https://img.shields.io/badge/Wails-v2.15-DF0000?style=flat-square" alt="Wails Version" />
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react" alt="React Version" />
  <img src="https://img.shields.io/badge/TailwindCSS-v4-06B6D4?style=flat-square&logo=tailwindcss" alt="Tailwind CSS" />
  <img src="https://img.shields.io/badge/Runtime-Bun%201.4-FBF0DF?style=flat-square&logo=bun" alt="Bun" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License" />
</p>

---

## 💡 项目简介

SheepGet 旨在解决现有下载工具弹窗杂乱、过度膨胀与侵入性过强的问题。它采用 Go 作为唯一可靠的底层调度核心，结合 Web 前端现代质感，为用户提供一个纯净、高效、易用的下载管理体验。

### 核心亮点

- **Modern Refined Utility UI**：参考 Linear + Raycast 的视觉哲学，高信息密度、克制精致，配合流畅的交互与状态过渡动画。
- **高性能传输核心**：Go 原生调度引擎，支持多并发动态分块、慢区块自适应再分配、严格的版本一致性校验与断点续传。
- **原生媒体流处理**：遵循独立媒体处理架构（Native Go Media Processing），无外部冗余依赖，原生组装常见点播 HLS (m3u8) / TS / fMP4。
- **集中化任务反馈**：独立的聚合下载进度窗口与小巧实用的媒体悬浮交互，告别满屏杂乱弹窗。
- **轻量无负担**：基于 Wails 原生 WebView 绑定，兼具跨平台桌面能力与原生级资源占用。

---

## 🛠️ 技术架构

| 层次 | 选型与关键库 | 职责定位 |
| :--- | :--- | :--- |
| **桌面基座** | [Wails v2](https://wails.io/) | 轻量级跨平台运行时与 Go-Web 双向 RPC 桥接 |
| **后端核心** | Go 1.27 | 业务规则、任务生命周期、队列调度与并发下载唯一事实来源 |
| **前端基座** | React 19 + TypeScript + Vite | 声明式界面开发与严格类型安全 |
| **样式与组件** | Tailwind CSS v4 + Radix UI 原语 + Lucide 图标 | 无头无障碍基础与高定制、像素级视觉设计 |
| **动画与动效** | Motion (Framer Motion) | 丝滑的任务状态过渡与动态重排布局 |
| **包管理工具** | Bun 1.4 | 现代化全栈 JS/TS 工具链与极速依赖管理 |

---

## 🚀 快速开始

### 前置环境

在开始之前，请确保本地已安装以下环境：

- **Go**：`>= 1.25`（建议使用 `1.27.x`）
- **Node.js**：`>= 22.x`（推荐 `24.x`）
- **Bun**：`>= 1.2`（推荐 `1.4.x`）
- **Wails CLI**：
  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

### 安装与运行

1. **克隆仓库**
   ```bash
   git clone https://github.com/ygq-future/SheepGet.git
   cd SheepGet
   ```

2. **安装前端依赖**
   ```bash
   bun install --cwd frontend
   ```

3. **启动开发调试模式**
   ```bash
   wails dev
   ```

4. **构建生产版本**
   ```bash
   wails build
   ```

---

## 🛡️ 代码质量与门禁 (Quality Gate)

项目建立了严格的 **Check-Only 全栈质量门禁体系** 与 Git 拦截钩子，提交前可随时执行统一自检：

```bash
bun run quality
```

该门禁按顺序严格覆盖：
1. `gofmt` 代码格式化检测
2. `go mod verify` 模块依赖一致性校验
3. `go vet` 编译器级静态分析
4. `go test` 单元测试运行
5. `tsc --noEmit` TypeScript 严格类型检查
6. `eslint` 规则与最佳实践校验
7. `prettier --check` 格式规范与 Tailwind 类名自动排序检查

---

## 📖 详细文档

- [产品需求与设计规格 (Spec)](docs/spec.md)
- [原生 Go 媒体处理架构决策 (ADR-0001)](docs/adr/0001-native-go-media-processing.md)
- [质量契约与门禁状态说明](docs/agents/quality.md)
- [领域模型与术语定义](CONTEXT.md)

---

## 📄 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。
