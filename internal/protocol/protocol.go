// Package protocol 是桌面端线上契约的唯一定义处：环回通道的端点路径、鉴权头、查询参数、
// 默认端口与端口顺延步长，以及两个事件通道（环回 WebSocket 与界面）的事件名。
//
// TypeScript 侧的镜像由 scripts/protocol.mjs 从这里生成（extension/lib/protocol.generated.ts
// 与 frontend/src/lib/protocol.generated.ts），门禁的 protocol 阶段在两侧不一致时失败：改了
// 一处线上事实却忘了同步另一侧，会让门禁直接报错，而不是让扩展在运行时静默连不上。
//
// 生成器按标了 `// mirror <目标>.<分组>` 的常量推导镜像，因此这一段的写法是契约的一部分：
//   - 镜像块里只写字面量、整数，或 `BasePath + "字面量"` 这种与本文件已声明常量的字符串拼接；
//   - 分组决定 TypeScript 侧的导出名（paths → Paths）与键名（去掉该分组前缀后原样保留），
//     新增线上事实时一并标上 mirror 目标；
//   - 不标 mirror 的常量不进入镜像（例如派生的 ExtensionOrigin）。
package protocol

// BasePath 是环回通道所有端点共用的前缀。
//
// mirror extension.basePath
const BasePath = "/api/v1"

// 环回端点路径。扩展与桌面端两侧都只认这里。
//
// mirror extension.paths
const (
	PathDiscover       = BasePath + "/discover"
	PathPing           = BasePath + "/ping"
	PathTakeoverConfig = BasePath + "/config/takeover"
	PathHandover       = BasePath + "/handover"
	PathHLSVariants    = BasePath + "/hls/variants"
	PathMediaProbe     = BasePath + "/media/probe"
	PathEvents         = BasePath + "/events"
)

// 请求与响应头。
//
// mirror extension.headers
const (
	HeaderToken         = "X-SheepGet-Token"
	HeaderConfigVersion = "X-Config-Version"
)

// 查询参数。事件长连接在地址上带会话令牌。
//
// mirror extension.query
const (
	QueryParamToken = "token"
)

// 默认监听端口与端口被占用时的顺延步长。
//
// mirror extension.ports frontend.ports
const (
	PortDefaultServer = 9248
	PortFallbackSpan  = 5
)

// 环回 WebSocket 事件名（桌面端 → 扩展）。
//
// mirror extension.wsEvents
const (
	EventTakeoverConfigUpdated = "takeover_config_updated"
	EventServerMigrated        = "server_migrated"
)

// 界面事件名（桌面端 → 前端，经 Wails 事件通道）。
//
// mirror frontend.events
const (
	EventTaskUpdated            = "task:updated"
	EventTaskDeleted            = "task:deleted"
	EventSettingsUpdated        = "settings:updated"
	EventProgressClearViewed    = "progress:clear_viewed"
	EventProgressFocusCompleted = "progress:focus_completed"
	EventProgressFocusTask      = "progress:focus_task"
	EventFileInfoNext           = "fileinfo:next"
	EventFileInfoQueueUpdated   = "fileinfo:queue_updated"
	EventFileInfoUpdated        = "fileinfo:updated"
	EventAppOpenSettings        = "app:open-settings"
	EventServerStatusChanged    = "server:status_changed"
)

// 扩展身份。ExtensionID 由 extension/wxt.config.ts 里的固定公钥推导而来（ADR-0005 决策 3），
// 生成器会用那份公钥重新推导一次并比对，两边不一致直接失败——换了公钥却忘了改这里，桌面端
// 会把扩展的请求当成陌生来源拒掉。
const (
	ExtensionID     = "oediboaeofmnlkgcjhnpfnngphkjooam"
	ExtensionOrigin = "chrome-extension://" + ExtensionID
)
