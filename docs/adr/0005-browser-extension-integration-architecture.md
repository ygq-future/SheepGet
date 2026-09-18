---
status: draft
---

# 浏览器扩展架构与桌面端通信集成

为使 SheepGet 能够可靠接管 Chrome 与 Edge 浏览器的普通文件与常见媒体下载，需要构建配套浏览器扩展并在扩展与桌面端之间建立低延迟、双向可控且支持断电/休眠自愈的通信体系。本 ADR 明确浏览器扩展的工程选型、通信通道、交接时序、配置同步、敏感凭据防护及快捷键捕获边界。

## 决策 1：交接确认的语义与状态回退机制

### 候选选项
- **选项 A（同步阻塞式暂缓 + ACK 取消）**：
  扩展捕获下载事件后，先通过 `chrome.downloads.pause(downloadId)` 将浏览器原生下载原位挂起，同时向桌面端发送交接请求。
  - 在超时时间（2.5 秒）内收到桌面端成功接收（已进入文件信息窗口单例队列）的交接确认（ACK）后，扩展调用 `chrome.downloads.cancel(downloadId)` 并擦除下载条痕迹；
  - 若桌面端超时无响应、进程未启动唤起失败、或显式拒绝，扩展立即调用 `chrome.downloads.resume(downloadId)` 恢复浏览器下载，并显示轻量 Toast 提示用户。
- **选项 B（先取消后异步重发）**：
  扩展拦截下载时立即执行 `chrome.downloads.cancel`，再异步投递任务；若后续交接失败，再调用 `chrome.downloads.download({ url })` 重新发起。

### 权衡与取舍
选项 B 存在严重的业务缺陷：重新发起的下载是一个全新的 HTTP GET 请求，原始请求上下文中的 POST Payload、一次性防刷校验 Token、单次有效 Cookie、防盗链特定 Header 均已丢失，极易引发 403 Forbidden、410 Gone 或产生表单重复提交的副作用。
选项 A 原位挂起原连接，连接链路和上下文在内核层得以保留。

### 决策结论
**采用选项 A（同步阻塞式暂缓 + 收到交接确认再 Cancel，超时/失败立即 Resume 恢复）**。

---

## 决策 2：通信通道与桌面端唤起机制

### 候选选项
- **选项 A（纯 Native Messaging + 独立 Host 二进制）**：
  通过 `browser.runtime.connectNative` 通信，依赖系统注册的 Native Messaging Host 唤起主程序。
- **选项 B（纯本地 Loopback 安全端口 + `sheepget://` 协议唤起）**：
  桌面端启动 HTTP/WebSocket 本地安全端口；未运行时扩展通过浏览器自定义协议 URL 唤起。
- **选项 C（双轨混合：本地 Loopback 为主通道 + Native Host 充当静默唤起跳板 + 协议/UI 降级）**：
  日常高频通信与状态同步走本地 Loopback；安装模式下注册轻量 `sheepget-host` 充当静默拉起跳板；便携模式或未注册环境通过协议/UI 提示用户。

### 权衡与取舍（修正伪论据）
不再采用“Loopback 比 Native Messaging 性能更快”这一在小包通信中并不成立的伪论据。本决策完全基于以下两项核心物理约束：
1. **便携模式（Portable Mode）兼容性硬约束（ADR-0003）**：
   便携版应用严禁擅自修改系统注册表（如 Windows `HKCU\Software\Google\Chrome\NativeMessagingHosts`）或在系统全局目录写入文件。若纯走 Native Messaging，便携版将因无 Host 注册而彻底瘫痪。免注册的本地 Loopback 是便携版维持通讯的唯一通道。
2. **安装模式下的静默唤起体验（Spec 第 14 条）**：
   在正式安装模式下，用户触发下载时期望应用能像成熟下载工具一样静默启动并弹出文件信息窗口。若使用浏览器自定义协议（`sheepget://`），浏览器内核出于安全策略必然弹出“是否允许打开 SheepGet？”的模态确认框，阻断流畅操作。通过已注册的极薄 Native Host 启动主程序能够实现无感静默唤起。

### 决策结论
**采用选项 C（双轨制混合通道）**：
1. 桌面端启动时在 `127.0.0.1` 开启本地 HTTP/WebSocket 安全通道（附带随机启动 Token 鉴权），承担日常下载交接与状态广播；
2. 安装版提供并注册轻量 `sheepget-host` 辅助可执行程序，作为 Native Messaging 静默唤起跳板；
3. 免安装便携版（未注册 Host 时）若检测到 Loopback 离线，则通过系统协议唤起或扩展气泡提示引导启动。

---

## 决策 3：扩展工程结构、质量门禁与扩展 ID 固定策略

### 候选选项
- **选项 A（根目录独立工程 `extension/`）**：
  根目录下创建独立 `extension/` 目录，采用 WXT 框架（TypeScript + React）构建，产物输出至根级 `dist-extension/`。
- **选项 B（合并进 `frontend/extension/`）**：
  作为 `frontend` 的子包共享 `node_modules` 与 `package.json`。

### 权衡与取舍
Wails v3 桌面端前端（Wails Bindings, SPA DOM）与 Chrome/Edge MV3 扩展（`webextension-polyfill`, Chrome Extension APIs）的运行时全局声明存在冲突（如全局 `window` 与 Chrome 命名空间）。独立工程能够保证类型边界干净，且便于独立构建和解压分发。

### 门禁与构建策略
1. **工程定位**：根目录 `/extension`，使用 WXT 框架，基于 Vite + TypeScript 构建。
2. **质量门禁接入（`scripts/quality-gate.mjs`）**：
   门禁脚本中新增扩展专属验证步骤：
   - 依赖与类型检查：`wxt prepare && tsc --noEmit`；
   - 生产构建检查：`wxt build`（确保能无警告产出有效的 MV3 目录）。
3. **扩展 ID 固定策略（钉死 ID）**：
   在开发者模式侧载时，若无显式公钥，扩展 ID 会随路径改变而重新计算，导致 Native Messaging Host 的 `allowed_origins` 失效。
   **规则**：在 `wxt.config.ts` 的 manifest 声明中配置固定的 `key` 字段（从仓库预设的扩展公钥派生），确保生成的扩展 ID 在所有开发与测试环境中全局恒定（例如固化为固定的 32 位扩展 ID），Host 白名单严格与该 ID 绑定。

---

## 决策 4：配置中心单一事实来源下的扩展规则同步（含 SW 休眠防坑）

### 物理约束
Chrome/Edge Manifest V3 的 Background Service Worker 会在**无事件 30 秒后被浏览器内核强制终止挂起**。休眠期间，桌面端即便在 WebSocket 上广播配置更新事件，Service Worker 也无法接收，内存状态直接丢失。

### 决策结论
**采用「唤醒主动校准 + 运行期事件推送 + 本地持久化缓存」的三级同步体系**：
1. **本地持久化缓存**：扩展在 `chrome.storage.local` 中维护一份 `TakeoverConfig` 镜像副本。
2. **休眠唤醒主动校准**：Service Worker 每次被浏览器内核唤醒（如收到下载事件、快捷键消息或定时心跳）：
   - 首先同步读取 `chrome.storage.local` 确保内存有底线规则可用，实现毫秒级本地过滤；
   - 异步通过 Loopback 发起轻量版本检查（带 `configVersion` / ETag），若桌面端配置已变更，即刻拉取最新配置并刷新 `chrome.storage.local`。
3. **活跃期即时推送**：在 Service Worker 存活的长连接期间，桌面端配置修改时即时广播推送，扩展收到后直接写回缓存。

---

## 决策 5：请求上下文契约与类型级默认脱敏（Secure by Default）

### 风险核查与原则
经核实，Go 后端尚无全局通用 logger，错误均通过 `fmt.Errorf` 向上透传；但任务模型（`internal/task/Task`）会直接序列化后通过 Wails 事件总线广播至前端 Webview。
如果仅靠约定提供 `LogSanitized()` 方法，极易因人为遗漏、`fmt.Printf("%+v")` 或序列化事件广播而将 Cookie / Authorization 泄露至控制台或前端内存。
**防御原则：靠结构与类型系统，不靠约定；默认脱敏，按需解密。**

### 决策结论
1. **凭据封装为受保护类型**：
   在 Go 后端将包含敏感信息的请求头从裸 `map[string]string` 替换为专属类型：
   ```go
   type RequestCredentials struct {
       headers map[string]string // 私有字段，禁止无防护遍历
   }
   ```
2. **序列化与格式化默认安全**：
   - 为该类型实现 `json.Marshaler`：在 JSON 序列化（含 Wails 窗口广播）时，仅白名单通用字段（如 `User-Agent`, `Referer`）原样输出，敏感字段（`Cookie`, `Authorization`, `Token`, `Secret`）自动转换为掩码（如 `Cookie: [present, 4 keys]`, `Authorization: Bearer ****`）；
   - 为该类型实现 `fmt.Stringer`：防止任何格式化打印输出明文凭据。
3. **明文凭据的单一出口**：
   仅提供传输层专用方法（如 `ApplyToHTTPRequest(req *http.Request)`），由核心下载引擎在组装实际出站请求时单独调用，彻底切断普通日志和界面层的明文暴露路径。

---

## 决策 6：按住快捷键捕获机制与特权页面物理边界

### 物理约束与参考实践
参考成熟扩展（如 IDM 官方扩展 `IDMGCExt`）的工业级实践：
- `chrome.commands` 仅支持瞬时触发，不支持检测按住与松开；
- 浏览器内核（出于安全防御）严禁向特权页面（`chrome://*`、`edge://*`、Web Store 页面、内部下载/设置页）注入 Content Script。

### 决策结论
1. **捕获机制**：
   - 在 `content.js` 中使用 `window.addEventListener('keydown' / 'keyup', ..., true)` 在**捕获阶段（Capture Phase）**监听按键状态；
   - 过滤长按重复事件（`!e.repeat`），将按键事件同步至 Background Service Worker 内存位掩码（Bitmask）；
   - 支持系统修饰键与控制键组合（`Shift`, `Ctrl`, `Alt`, `Insert`, `Delete`）。
2. **防卡死防御机制**：
   - 页面失去焦点（`window.addEventListener('blur', ...)`）时，强制向 background 发送清空消息将掩码归零；
   - 浏览器标签页激活切换（`chrome.tabs.onActivated`）及窗口焦点变化时，自动重置按键掩码，彻底防止切屏导致的“按键卡在按住状态”。
3. **不可行物理边界确认**：
   - 明确特权页面（`chrome://`、`edge://` 等）无法注入 Content Script；在特权页面触发下载时，物理上无法感知按键状态，系统自动降级为**按全局常规接管配置执行**。该限制符合浏览器安全沙箱规范，记录为客观系统边界。
