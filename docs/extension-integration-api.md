# SheepGet 浏览器扩展通用投递 API 规范与集成指南

## 1. 概述与设计定位

SheepGet 浏览器扩展具备强大的网络层（`webRequest.onHeadersReceived`）与 DOM 渲染层双轨音视频与文件嗅探能力。为了不仅服务于 SheepGet 桌面下载管理器，同时能够作为**独立通用媒体嗅探工具**发布在 Chrome Web Store，扩展内置了**多通道解耦投递架构**。

扩展支持四种运行与投递模式：
1. **SheepGet 原生客户端（默认模式）**：通过本地回环自动握手与 WebSocket 长连接，实现零配置秒级投递与接管规则实时同步。
2. **浏览器原生下载（单机零依赖模式）**：无需任何外部客户端，直接调用 Chrome 原生下载接口完成直链保存。
3. **Aria2 / Motrix JSON-RPC 通道**：标准 JSON-RPC 2.0 协议投递，一键联动 Aria2、Motrix、Aria2c 及群晖/QNAP 等 NAS 下载服务。
4. **通用 HTTP Webhook（REST API 通道）**：向任意用户自定义的 HTTP(S) 端点发送标准 JSON 负载，适合个人开发者、自建脚本、第三方下载工具快速集成。

此外，扩展在离线或无客户端环境下，提供一键**复制媒体直链**与**复制携带完整 Referer、User-Agent、Cookie 凭据的标准 cURL 命令行**功能。

---

## 2. 通用 HTTP Webhook 协议规范 (REST API)

当用户在扩展设置中选择目标为「通用 HTTP Webhook」时，点击下载或接管下载将向配置的 URL 发起 HTTP 请求。

### 2.1 请求基本规格

- **请求方式**：`POST`（默认）或 `PUT`（可配置）
- **Content-Type**：`application/json; charset=utf-8`
- **鉴权 Header**：若用户配置了 Token，扩展将自动添加：
  ```http
  Authorization: Bearer <your_token>
  ```
  *(若 Token 已包含 `Bearer ` 前缀，则保持原样传递；支持在配置中扩充自定义 Header)*

### 2.2 请求数据结构 (HandoverRequest JSON)

```json
{
  "sourceType": "resource_list",
  "url": "https://media.example.com/stream/video.mp4",
  "filenameSuggestion": "2026年最新视频.mp4",
  "totalBytes": 104857600,
  "mimeType": "video/mp4",
  "pageContext": {
    "pageUrl": "https://www.example.com/watch?v=98765",
    "referrer": "https://www.example.com/",
    "pageTitle": "视频播放页面标题"
  },
  "credentials": {
    "cookies": "SESSION_ID=abc123xyz; secure_token=def456",
    "headers": {
      "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36...",
      "Referer": "https://www.example.com/watch?v=98765"
    }
  },
  "mediaMeta": {
    "isHls": false
  },
  "variantUri": "https://media.example.com/stream/1080p.m3u8"
}
```

### 2.3 字段详细字典

| 字段名 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| `sourceType` | `string` | 是 | 触发来源：`"resource_list"` (Popup面板点击)、`"media_bar"` (网页悬浮条点击)、`"browser_takeover"` (下载拦截接管) |
| `url` | `string` | 是 | 媒体或下载资源的真实原始 URL |
| `filenameSuggestion` | `string` | 否 | 建议保存的文件名（已解析响应头 Content-Disposition 或页面标题，过滤非法字符） |
| `totalBytes` | `number` | 否 | 响应头声明的资源总字节数（分块传输或未知时可能为空） |
| `mimeType` | `string` | 否 | 响应头返回的 MIME 类型，例如 `video/mp4`、`application/vnd.apple.mpegurl` |
| `pageContext.pageUrl` | `string` | 是 | 发现该资源的浏览器顶层网页真实 URL |
| `pageContext.pageTitle` | `string` | 否 | 网页标题（已自动清洗首尾空白） |
| `pageContext.referrer` | `string` | 否 | 网页来源或上级页面地址 |
| `credentials.cookies` | `string` | 否 | 该域名下携带的完整有效 Cookie 字符串（分号分隔，防盗链必需） |
| `credentials.headers` | `object` | 否 | 下载所需的关键请求头，固定包含真实的 `User-Agent` 与 `Referer` |
| `mediaMeta.isHls` | `boolean` | 否 | 是否为 HLS (m3u8) 流媒体资源 |
| `variantUri` | `string` | 否 | 若该媒体为多清晰度 HLS，且用户在 UI 选定了具体清晰度，则为选中的子清单 URL |

### 2.4 响应契约 (Response Schema)

接收端服务处理完成后，应返回 HTTP `200` ~ `204` 状态码，并建议返回如下 JSON 响应：

```json
{
  "accepted": true,
  "queueItemId": "task_1001"
}
```

若拒绝或失败，返回 HTTP `4xx`/`5xx` 或 `accepted: false`：

```json
{
  "accepted": false,
  "reason": "磁盘空间不足或任务已存在"
}
```

---

## 3. Aria2 JSON-RPC 协议集成

对于使用 Aria2、Motrix、Aria2-Pro 或 NAS 下载站的用户，扩展直接提供原生 JSON-RPC 2.0 映射：

- **调用方法**：`aria2.addUri`
- **参数映射规则**：
  - `params[0]`：若配置了 Secret，则为 `token:<secret>`；
  - `params[1]`：`[url]`（待下载资源地址）；
  - `params[2]`：下载选项字典，自动映射：
    - `out`：映射自 `filenameSuggestion`
    - `dir`：映射自用户配置的默认保存目录
    - `header`：自动打包数组，包括 `User-Agent: ...`、`Referer: ...`、`Cookie: ...`

---

## 4. 极简服务端接入示例

### Python (FastAPI) 示例

```python
from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel
from typing import Optional, Dict, Any

app = FastAPI()

class HandoverPayload(BaseModel):
    url: str
    filenameSuggestion: Optional[str] = None
    pageContext: Optional[Dict[str, Any]] = None
    credentials: Optional[Dict[str, Any]] = None

@app.post("/download")
async def receive_download(req: HandoverPayload, authorization: Optional[str] = Header(None)):
    # 鉴权示例（如需）
    if authorization and authorization != "Bearer my-secret-token":
        raise HTTPException(status_code=401, detail="Unauthorized")

    print(f"收到下载任务: {req.filenameSuggestion} <- {req.url}")
    # 在此调用您的下载引擎或将任务存入数据库...
    return {"accepted": True, "queueItemId": "task_demo_123"}
```

### Node.js (Express) 示例

```javascript
import express from 'express';
const app = express();
app.use(express.json());

app.post('/download', (req, res) => {
  const { url, filenameSuggestion, credentials } = req.body;
  console.log(`[Webhook] 下载任务已就绪: ${filenameSuggestion} (${url})`);
  
  // 获取 Cookies 和 Referer（防盗链）
  const headers = credentials?.headers || {};
  const cookies = credentials?.cookies || '';
  
  res.json({ accepted: true, queueItemId: `task_${Date.now()}` });
});

app.listen(8080, () => console.log('下载接收端已就绪: http://localhost:8080/download'));
```

### Go 示例

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type HandoverRequest struct {
	URL                string `json:"url"`
	FilenameSuggestion string `json:"filenameSuggestion"`
	Credentials        struct {
		Cookies string            `json:"cookies"`
		Headers map[string]string `json:"headers"`
	} `json:"credentials"`
}

func main() {
	http.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		var req HandoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("收到下载任务: %s (%s)", req.FilenameSuggestion, req.URL)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
	})
	log.Println("服务启动于 :8080")
	_ = http.ListenAndServe(":8080", nil)
}
```

---

## 5. 离线独立工具能力 (Zero-Client Utility)

即便不启动任何外部服务或客户端，SheepGet 浏览器扩展本身也是一个高效率的生产力嗅探工具：
- **浏览器内置下载**：在 Popup 列表中点击单个资源的“蓝色下载图标”，或将全局目标设为“浏览器内置下载”，可直接借助浏览器的下载管理器平替 IDM；
- **复制直链**：在网页播放器右上角悬浮条右键点击，或在 Popup 列表中点击“链接”图标，一秒提取真实音视频直链或选定清晰度 M3U8；
- **复制标准 cURL**：点击“终端代码”图标，自动生成并复制形如 `curl -L -o "title.mp4" -H "Referer: ..." -H "Cookie: ..." "https://..."` 的完整终端命令行，便于服务器运维与开发者快速下载。
