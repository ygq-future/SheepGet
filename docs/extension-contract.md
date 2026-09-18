# 浏览器扩展与桌面端双向通信数据契约规范

本规范定义 SheepGet 浏览器扩展（Chrome / Edge）与 SheepGet 桌面端之间的双向通信协议、消息类型、字段级数据模型与敏感信息安全处理规则。

---

## 1. 通信基础与传输协议

- **通信通道**：
  - **HTTP/JSON API（主通道）**：监听于 `http://127.0.0.1:<port>/api/v1/...`，桌面端启动时动态分配空闲端口，并在应用运行目录（或已注册 Host 跳板）写入本地会话文件（含端口与随机分配的 `sessionToken`）；
  - **WebSocket API（事件总线）**：`ws://127.0.0.1:<port>/api/v1/events?token=<sessionToken>`，承载配置变更推送、桌面端就绪状态及双向心跳。
  - **Native Messaging 跳板（唤起通道）**：Chrome/Edge 标准 stdio 协议，格式为 4 字节长度前缀 + JSON 报文，仅用于在 SheepGet 未运行时拉起主程序并返回当前活动的 Loopback 端口与会话凭证。

- **通用请求头**：
  所有发送到桌面端的 HTTP 请求必须包含安全头：
  - `X-SheepGet-Token: <sessionToken>`
  - `Content-Type: application/json`

---

## 2. 消息类型与端点定义

| 端点 / 动作                      | 协议 | 发起方 | 描述                                                  |
| :------------------------------- | :--- | :----- | :---------------------------------------------------- |
| `POST /api/v1/handover`          | HTTP | 扩展   | 提交候选下载任务进行交接确认（Handover Confirmation） |
| `GET /api/v1/config/takeover`    | HTTP | 扩展   | Service Worker 唤醒时主动拉取或校准当前规则           |
| `GET /api/v1/ping`               | HTTP | 扩展   | 探测桌面端可用性与鉴权有效性                          |
| `EVENT: takeover_config_updated` | WS   | 桌面端 | 配置中心发生规则变更时即时广播                        |

---

## 3. 字段级数据契约

### 3.1 候选任务交接请求（`HandoverRequest`）

扩展拦截到下载事件、或用户在媒体悬浮条/资源列表主动点击下载时，发送此结构：

```json
{
  "sourceType": "browser_takeover",
  "url": "https://example.com/files/archive.zip",
  "filenameSuggestion": "archive.zip",
  "totalBytes": 104857600,
  "mimeType": "application/zip",
  "pageContext": {
    "pageUrl": "https://example.com/download.html",
    "referrer": "https://example.com/index.html",
    "pageTitle": "Download Archive - Example"
  },
  "credentials": {
    "cookies": "SESSIONID=xyz123; user_pref=dark",
    "headers": {
      "User-Agent": "Mozilla/5.0 ...",
      "Authorization": "Bearer token_secret_456",
      "Referer": "https://example.com/download.html"
    }
  },
  "mediaMeta": null
}
```

#### 字段定义与敏感度级别：

| 字段路径                | 类型                     | 必需 | 敏感度          | 约束与说明                                                                               |
| :---------------------- | :----------------------- | :--- | :-------------- | :--------------------------------------------------------------------------------------- |
| `sourceType`            | `string`                 | 是   | 公开            | 枚举：`browser_takeover`（自动接管）、`media_bar`（悬浮条）、`resource_list`（扩展列表） |
| `url`                   | `string`                 | 是   | 受限            | 目标资源完整 HTTP/HTTPS 链接                                                             |
| `filenameSuggestion`    | `string`                 | 否   | 公开            | 浏览器或页面建议的文件名（优先于 URL 解析出的名字）                                      |
| `totalBytes`            | `number`                 | 否   | 公开            | 若未知或未探测到则为 `-1` 或 `0`                                                         |
| `mimeType`              | `string`                 | 否   | 公开            | Content-Type 响应头或扩展探测的 MIME 类型                                                |
| `pageContext.pageUrl`   | `string`                 | 是   | 公开            | 发起下载的宿主页面 URL（用于站点排除判定与来源展示）                                     |
| `pageContext.referrer`  | `string`                 | 否   | 公开            | 页面 Referer 请求头                                                                      |
| `pageContext.pageTitle` | `string`                 | 否   | 公开            | 页面标题，用于文件信息窗口中展示辅助信息                                                 |
| `credentials.cookies`   | `string`                 | 否   | **[敏感]**      | 页面 Cookie，严禁写入普通日志与前端事件                                                  |
| `credentials.headers`   | `Record<string, string>` | 否   | **[敏感/受限]** | 自定义头字典。其中 `Authorization`、`Cookie`、`Token` 为核心敏感                         |
| `mediaMeta`             | `object`                 | 否   | 公开            | 仅用于媒体直链或 m3u8 清单，包含清晰度标识与流格式                                       |

---

### 3.2 任务交接响应（`HandoverResponse`）

桌面端在成功将任务排入文件信息窗口单例队列、或做出重复策略裁决后，同步返回：

```json
{
  "accepted": true,
  "queueItemId": "item_1742000000_abc",
  "reason": ""
}
```

- `accepted: true`：扩展收到后立即执行 `chrome.downloads.cancel(downloadId)`，交接完成；
- `accepted: false`：桌面端显式拒绝（例如 URL 格式非法、用户本地策略完全禁用等），扩展执行 `chrome.downloads.resume(downloadId)` 恢复浏览器原生下载。

---

### 3.3 接管规则配置模型（`TakeoverConfigSync`）

与后端 `TakeoverConfig` 镜像，扩展存储在 `chrome.storage.local` 中：

```json
{
  "version": 1742000000,
  "extensions": ["zip", "rar", "7z", "mp4", "mkv", "iso"],
  "excludedSites": ["github.com", "bank.example.com"],
  "pauseShortcut": "Delete",
  "forceShortcut": "Insert"
}
```

- `version`：递增版本戳（秒级或毫秒级时间戳）。Service Worker 唤醒时先通过 `HEAD /api/v1/config/takeover` 检查版本，版本一致则直接复用本地缓存。

---

## 4. 敏感凭据脱敏防线规约（Secure by Default）

为严格执行 Ticket 06 验收标准：_“传递必要 URL、Cookie、Referer 和 Header 等上下文，避免把请求凭据暴露在普通日志”_，数据结构遵循以下强制规约：

1. **白名单准入规则**：
   只有经过明确白名单准入的 Header（如 `User-Agent`、`Referer`、`Accept`、`Range`、`Accept-Language`）允许明文保存在通用任务详情与日志展示中。
2. **黑名单强制掩码**：
   凡 Header 键名包含下列模式（大小写无关）：
   - `*cookie*`
   - `*authorization*`
   - `*token*`
   - `*secret*`
   - `*signature*`
   - `*key*`
     必须在任何日志输出、控制台打印、Wails Webview 窗口事件广播时自动掩码（Masking）。
3. **掩码规则示例**：
   - 原始：`Cookie: id=abc123456789; token=secret999`
     掩码：`Cookie: [present, 2 items, 38 chars]`
   - 原始：`Authorization: Bearer eyJhbGciOi...`
     掩码：`Authorization: Bearer ****`
4. **底层类型封锁**：
   Go 后端严禁使用裸 `map[string]string` 保存或透传包含敏感凭据的 Headers，统一使用封装结构 `RequestCredentials`，其默认序列化逻辑（`MarshalJSON` 与 `String`）只产生脱敏数据，物理切断因无意调用导致的泄露通道。
