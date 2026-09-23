// 由 scripts/protocol.mjs 从 internal/protocol/protocol.go 生成，请勿手改。
// 改动线上事实请改 Go 侧那个文件，再运行 node scripts/protocol.mjs --write。

export const BasePath = '/api/v1';

export const Paths = {
  Discover: '/api/v1/discover',
  Ping: '/api/v1/ping',
  TakeoverConfig: '/api/v1/config/takeover',
  Handover: '/api/v1/handover',
  HLSVariants: '/api/v1/hls/variants',
  MediaProbe: '/api/v1/media/probe',
  Events: '/api/v1/events',
} as const;

export const HeaderNames = {
  Token: 'X-SheepGet-Token',
  ConfigVersion: 'X-Config-Version',
} as const;

export const QueryParams = { Token: 'token' } as const;

export const Ports = { DefaultServer: 9248, FallbackSpan: 5 };

export const WsEvents = {
  TakeoverConfigUpdated: 'takeover_config_updated',
  ServerMigrated: 'server_migrated',
} as const;
