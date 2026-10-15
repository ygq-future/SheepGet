# 下载引擎高可用与全链路兜底演进蓝图

本文档记录 SheepGet 下载引擎的高可用防线架构、现有能力盘点、关键盲区分析及 P0–P2 分阶段演进进度。

---

## 一、 五层高可用防线模型

1. **入口与交接防线**：浏览器接管两阶段确认、环回通道端口顺延自愈、预探测超时盲下兜底。
2. **网络与协议防线**：代理 ↔ 直连动态故障逃生、协议探测降级、302 临时签名过期自动回源刷新。
3. **并发与流控防线**：慢启动探测（Slow Start）、429/503 与连接异常动态缩容、慢块折半拆分、空闲看门狗（StallWatch）斩断僵尸连接。
4. **数据一致性与校验防线**：ETag / Last-Modified 强校验防脏拼、假 Range（200 OK）识别、分块边界截断保护。
5. **磁盘与生命周期防线**：临时文件原子替换、Windows 文件锁共享冲突退避重试、跨盘安全转移、HLS 传输与处理阶段分离恢复。

---

## 二、 现有已落地能力盘点（基线）

- [x] **并发慢启动与 429/503 限流降级**（`internal/engine/adaptive.go`）
- [x] **单线故障退避隔离与健康协程替补**（`internal/engine/downloader.go`）
- [x] **局部慢块 ETA 动态折半拆分 + 整体限速防碎保护**（`internal/engine/downloader.go`）
- [x] **无字节空闲看门狗（HTTP 30s / HLS 15s）**（`internal/stallwatch/stallwatch.go`）
- [x] **探测阶段（Probe）代理失败回退直连**（`internal/engine/downloader.go`）
- [x] **Range GET → HEAD 兜底 → 探测超时转未知大小排队下载**（`internal/engine/downloader.go`, `manager.go`）
- [x] **ETag / Last-Modified 续传与换链强校验**（`internal/engine/downloader.go`, `manager.go`）
- [x] **跨盘安全复制与 HLS 传输/处理阶段隔离恢复**（`internal/engine/fileops.go`, `hls.go`）

---

## 三、 演进路线图与实施进度

### 🔥 P0 梯队：核心传输韧性补齐（已完成）

| 编号     | 兜底措施                                     | 涉及模块                                                                                                                            |   状态    | 核心验收标准与落盘验证                                                                                                                                                                                                                                                                                                                                              |
| :------- | :------------------------------------------- | :---------------------------------------------------------------------------------------------------------------------------------- | :-------: | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **P0-1** | **全链路“代理 ↔ 直连”传输中故障逃生**        | `internal/engine/downloader.go`<br/>`internal/engine/hls.go`<br/>`internal/hls/download.go`<br/>`internal/hls/inspect.go`           | ✅ 已完成 | 1. HTTP 分块/单流传输中途若代理崩溃或断流，自动通过直连探针验证并无缝切换为直连（`DirectTransfer = true`）继续下载，不丢失已下载进度；<br/>2. HLS 探测、解析与分片下载全面支持代理失败自动回退直连及继承 `Task.DirectTransfer`。<br/>3. 测试验证：`TestHTTPDownloader_MidTransferProxyFallbackToDirect`、`TestFetcher_WithFallbackSwitchesToDirectOnProxyFailure`。 |
| **P0-2** | **多维度并发缩容（连接重置/超时/403 风暴）** | `internal/engine/adaptive.go`<br/>`internal/engine/downloader.go`                                                                   | ✅ 已完成 | 1. 除 `429/503` 外，当服务端通过直接断开连接（RST / EOF / 建连超时）或并发 `403`（在已有连接成功拉流且多并发活跃时）限制连接数时，`AdaptiveConcurrency` 识别连接风暴并动态下调并发上限；<br/>2. 避免高并发配置在严苛 CDN/防火墙下耗尽重试次数导致任务失败。<br/>3. 测试验证：`TestHTTPDownloader_AdaptiveConcurrency_ConnResetAnd403Storm`。                        |
| **P0-3** | **过期重定向（`FinalURL`）自动回源刷新**     | `internal/engine/downloader.go`                                                                                                     | ✅ 已完成 | 1. 当任务使用 302 重定向后的 `FinalURL`（`FinalURL != URL`）传输或续传遭遇 `401/403/410` 等过期状态码时，不立即判死；<br/>2. 自动携带原 `RequestHeaders` 回源请求 `t.URL` 刷新 `FinalURL`，校验同一文件版本（ETag/Last-Modified/Size）后无缝恢复分块传输。<br/>3. 测试验证：`TestHTTPDownloader_ExpiredFinalURLRefreshFromOrigin`。                                 |
| **P0-4** | **Windows 文件原子重命名退避与防误删保护**   | `internal/atomicfile/atomicfile.go`<br/>`internal/engine/fileops.go`<br/>`internal/hls/download.go`<br/>`internal/media/process.go` | ✅ 已完成 | 1. 统一 `atomicfile.Rename`：遇到短暂文件句柄占用（如 Windows Defender 扫描或缩略图索引引起的 Sharing Violation / Access Denied）时自动指数退避重试（目录冲突立即失败）；<br/>2. HLS 分片重命名失败时先退避重试，且避免在校验通过后因目标重命名瞬态失败而误删有效 `.part` 数据。<br/>3. 测试验证：`TestRenameReplacesTargetAndFailsFastOnDirectory`。               |

---

### 🛡️ P1 梯队：协议降级与 HLS 引擎加固（已完成）

| 编号     | 兜底措施                                                | 涉及模块                                                                                                                  |   状态    | 核心验收标准与落盘验证                                                                                                                                                                                                                                                                                                                                                                                                                  |
| :------- | :------------------------------------------------------ | :------------------------------------------------------------------------------------------------------------------------ | :-------: | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **P1-1** | **“假 Range（200 OK）”自动降级单流与单流断点续传升级**  | `internal/engine/downloader.go`                                                                                           | ✅ 已完成 | 1. 分片请求遭遇服务端忽略 `Range` 返回 `200 OK`（`ErrRangeNotSupported`）时，自动重置分块并无缝降级为单流下载，不直接判任务失败；<br/>2. 单流下载中途从响应头识别出 `Content-Length` 与 `Accept-Ranges: bytes` 后若发生断流或暂停，保留已落盘进度并升级为 Range 分块断点续传。<br/>3. 测试验证：`TestHTTPDownloader_IgnoreRange`、`TestHTTPDownloader_SingleStreamUpgradesToResumableChunksOnMidDrop`。                                 |
| **P1-2** | **HLS 分片指数退避重试与自适应并发流控**                | `internal/hls/download.go`                                                                                                | ✅ 已完成 | 1. 消除 HLS 分片重试的 0 延迟紧循环，对瞬态故障（5xx / 429 / 408 / 网络停摆）引入指数退避等待，确定性 4xx（如 404/403）快速失败；<br/>2. 引入 `segmentFlowGate` 自适应并发窗口，遭遇 429/503 或连续失败时动态下调并发，平稳传输后逐步恢复。<br/>3. 测试验证：`TestDownload_SegmentExponentialBackoffAndFatalFastFail`、`TestDownload_AdaptiveConcurrencyUnderRateLimit`。                                                               |
| **P1-3** | **磁盘可用空间前置预检（Pre-flight Disk Space Check）** | `internal/sys/disk*.go`<br/>`internal/engine/fileops.go`<br/>`internal/engine/downloader.go`<br/>`internal/engine/hls.go` | ✅ 已完成 | 1. 新增跨平台磁盘可用空间查询与同卷判定（Windows `GetDiskFreeSpaceEx` / Unix `Statfs`），支持未创建子目录向上回溯及续传差额抵扣；<br/>2. 在 HTTP 预分配/单流解析出大小、跨盘复制、HLS 分片拉取及媒体合并前校验临时卷与目标卷可用空间，空间不足时提前拦截并保留已有分片。<br/>3. 测试验证：`TestAvailableDiskSpaceAndPreFlightCheck`、`TestHTTPDownloader_PreFlightDiskSpaceCheck`、`TestManager_HLSProcessingPreFlightDiskSpaceCheck`。 |

---

### 🧩 P2 梯队：异常感知与长周期自愈（远期规划）

| 编号     | 兜底措施                                            | 涉及模块                                                       |   状态    | 核心目标                                                                                  |
| :------- | :-------------------------------------------------- | :------------------------------------------------------------- | :-------: | :---------------------------------------------------------------------------------------- |
| **P2-1** | **HTML 错误页/登录页伪装识别（200 OK 软错误拦截）** | `internal/engine/downloader.go`                                | 📋 待启动 | 防止需鉴权链接过期返回 `200 OK` + `text/html` 登录页被静默保存为损坏的二进制/安装包文件。 |
| **P2-2** | **HLS 长连接池复用与连接关闭自愈调优**              | `internal/engine/downloader.go`<br/>`internal/hls/download.go` | 📋 待启动 | 优化上千个小分片场景下的 Keep-Alive 连接复用率，减少高频 TLS 握手开销。                   |
