# SheepGet 浏览器扩展 (Chrome / Edge)

SheepGet 浏览器扩展是一个**通用、高效、精致的现代音视频嗅探与下载接管工具**。

它既可以与 [SheepGet 桌面下载管理器](https://github.com/ygq-future/SheepGet) 深度联动，也可以作为**独立、通用**的音视频嗅探扩展，无缝集成到 Aria2、Motrix、通用 Webhook 接收端或纯浏览器单机环境中。

---

## 核心特性

- 🎯 **双轨媒体嗅探**：
  - **网络层监听**：基于 `chrome.webRequest.onHeadersReceived` 实时捕获真实媒体请求与流媒体清单（MP4、WebM、FLV、M3U8 等）；
  - **DOM 树主动扫描**：自动感知动态插入的 HTML5 `<video>` / `<audio>` 标签。
- 📺 **智能网页悬浮条 (MediaBar)**：
  - 自动停靠在播放器右上角外侧，不遮挡画面；
  - 支持多码率 HLS 清单即时展开清晰度菜单；
  - **左键一键下载，右键直接复制音视频直链**。
- 🚀 **四重通用投递通道（在扩展设置中自由切换）**：
  1. **SheepGet 原生客户端**：基于本地回环与自动端口顺延探测（9248+），零配置即用；
  2. **浏览器内置下载**：无需启动任何客户端，直接调用浏览器下载引擎；
  3. **Aria2 / Motrix JSON-RPC**：支持直接向 `http://127.0.0.1:6800/jsonrpc` 投递带 Cookie 与 Referer 的下载任务；
  4. **通用 HTTP Webhook**：支持向任意自建 REST API 服务发送包含完整凭据的标准 JSON 请求。
- 📋 **开发者友好离线工具**：
  - 一键复制已嗅探媒体真实直链；
  - 一键生成携带防盗链 Header（`Referer`、`User-Agent`、`Cookie`）的完整 `cURL` 命令行。

---

## 开发者与集成协议

详细的 Webhook JSON 字段定义、Aria2 映射规则及 Python / Node.js / Go 极简服务端接入代码，请参阅：
👉 [SheepGet 浏览器扩展通用投递 API 规范与集成指南](../../docs/extension-integration-api.md)

---

## 构建与测试

扩展基于 [WXT (Next-gen Web Extension Framework)](https://wxt.dev/) 与 React 构建：

```bash
# 进入扩展目录
cd extension

# 类型检查与编译准备
bun run compile

# 运行单元测试
bun test

# 生产环境打包 (生成在 build/dist-extension/chrome-mv3)
bun run build
```
