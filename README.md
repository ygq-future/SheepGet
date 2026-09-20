# SheepGet

<p align="center">
  <strong>专为桌面用户打造的现代轻量、高性能下载管理器</strong>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
  <img src="https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey.svg" alt="Platform">
  <img src="https://img.shields.io/badge/Browser-Chrome%20%7C%20Edge-brightgreen.svg" alt="Browser">
</p>

---

SheepGet 是一款面向 **Windows、macOS 和 Linux** 的现代化桌面下载工具。它致力于提供克制精致的界面设计、极速稳定的多线程下载体验以及深度的浏览器流媒体嗅探与下载接管能力。

---

## 功能亮点

### ⚡ 高速多线程分块下载

- **动态多连接加速**：支持自定义并发连接数，充分利用网络带宽；
- **智能拖尾协助**：自动感知长尾慢区块并动态协助抓取，提升多通道利用率；
- **可靠断点续传**：支持暂停、继续与网络波动自动重试，支持过期链接原地更新续传；
- **跨盘安全转存**：临时下载目录与保存目录跨盘存放时自动校验转存，保障数据完整。

### 🎬 网页视频与流媒体下载

- **原生点播封装**：内置流媒体处理能力，主流点播音视频流与分片自动合并封装为标准 MP4，开箱即用；
- **播放器快捷悬浮条**：自动关联网页视频播放器，悬浮条停靠在播放器右上角外侧，鼠标悬停直接滑出清晰度菜单；
- **清晰度预选与体积预估**：支持在下载前选择目标画质，并直观展示清晰度对应的预估文件大小；
- **多清晰度独立归档**：多画质按需选择，自动对齐音视频轨并无损生成成品文件。

### 🧩 浏览器无缝协同

- **智能下载接管**：配套 Chrome 与 Edge 扩展，按文件后缀或规则自动接管普通文件下载，桌面端未就绪时自动交还浏览器；
- **站点排除与快捷键**：支持自定义不接管的站点名单，支持按住快捷键临时强制接管或暂停接管；
- **资源嗅探面板**：扩展工具栏图标一键查看当前标签页嗅探到的所有音频、视频及流媒体资源。

### 🪟 专注清爽的窗口体验

- **独立任务弹窗**：触发下载时弹出独立小巧的新建窗口，不打扰当前工作区焦点；支持多任务排队与自由切换；
- **共享下载浮窗**：多任务共用一个下载进度窗口，实时展示分段进度条，任务完成后置顶提供打开文件与定位目录入口；
- **托盘静默运行**：主窗口支持最小化与关闭后常驻系统托盘，后台任务与监听保持静默运行。

### 📁 自动分类与便携模式

- **智能后缀归档**：预置视频、音乐、图片、压缩包、软件等分类，支持自定义分类与独立保存目录；
- **绿色便携支持**：支持便携模式，数据和配置保存在程序同级目录，随时拷走即用。

---

## 浏览器配套扩展安装

SheepGet 提供随程序附带的浏览器配套扩展（Manifest V3），支持 Google Chrome 与 Microsoft Edge。

### 1. 获取扩展文件

通过项目构建命令生成扩展产物：

```bash
bun run --cwd extension build
```

构建生成的扩展文件夹位于：**`dist-extension/chrome-mv3/`**。

### 2. 加载扩展到浏览器

#### Google Chrome：

1. 打开 Chrome，在地址栏输入 `chrome://extensions/` 并回车；
2. 开启页面右上角的「**开发者模式**」（Developer mode）；
3. 点击左上角「**加载已解压的扩展程序**」（Load unpacked）；
4. 选择本项目根目录下的 `dist-extension/chrome-mv3` 文件夹完成加载；
5. 核对扩展列表中的 **SheepGet Integration Module**，扩展 ID 应为：
   ```text
   oediboaeofmnlkgcjhnpfnngphkjooam
   ```

#### Microsoft Edge：

1. 打开 Edge，在地址栏输入 `edge://extensions/` 并回车；
2. 开启左侧菜单栏下方的「**开发人员模式**」开关；
3. 点击「**加载解压缩的扩展**」；
4. 选择 `dist-extension/chrome-mv3` 文件夹完成加载。

### 3. 配置桌面端静默唤起（可选）

如需在桌面端未打开时通过浏览器操作直接唤起 SheepGet，可注册本地通信跳板：

- **Windows 系统**：以普通用户身份运行 PowerShell 脚本：

  ```powershell
  powershell -ExecutionPolicy Bypass -File scripts/register-host-windows.ps1
  ```

  _(如需卸载，运行 `powershell -File scripts/unregister-host-windows.ps1`)_

- **macOS / Linux 系统**：在终端运行注册脚本：
  ```bash
  ./scripts/register-host-posix.sh
  ```

---

## 源码构建与运行

如果您希望自行从源码构建或参与开发：

### 前置环境

- **Node.js**：`24.21.0` 或更高版本
- **Bun**：`v1.4.2` 或更高版本
- **Go**：`1.27.1` 或更高版本
- **CGO 编译环境**：Windows 推荐 MinGW-w64 (GCC)，Linux 需安装 `libgtk-3-dev` 与 `libwebkit2gtk-4.0-dev`，macOS 需安装 Xcode 命令行工具。

### 1. 初始化项目

克隆仓库后，在根目录执行初始化脚本以安装依赖和工具链：

```bash
node scripts/bootstrap.mjs
```

### 2. 本地开发与启动

启动开发热重载服务器（Go 后端与前端界面联动）：

```bash
bun run dev
```

### 3. 代码质量门禁

提交前运行质量门禁确保所有校验与测试通过：

```bash
bun run quality
```

---

## 开源协议

本项目基于 [MIT License](LICENSE) 协议开源。
