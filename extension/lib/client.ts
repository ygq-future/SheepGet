import type {
  HandoverRequest,
  HandoverResponse,
  HLSVariantsResponse,
  MediaProbeInfo,
  SessionMetadata,
  TakeoverConfigSync,
} from './types';
import { HeaderNames, Paths, Ports, QueryParams, WsEvents } from './protocol.generated';
/**
 * 直接通过本地 HTTP 探测桌面端会话。
 * 在便携版未注册 Host 或纯 HTTP 模式下，直接探测可实现秒连且零系统注册表侵入。
 */
export async function discoverSessionViaHttp(
  candidatePort: number = Ports.DefaultServer,
  fetchFn: typeof fetch = fetch,
): Promise<SessionMetadata | null> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 600);
  try {
    const res = await fetchFn(`http://127.0.0.1:${candidatePort}${Paths.Discover}`, {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json',
        'X-SheepGet-Client': 'extension',
      },
      signal: controller.signal,
    });
    if (!res.ok) {
      console.warn(
        `[SheepGet] Discover probe returned HTTP ${res.status} on port ${candidatePort}`,
      );
      return null;
    }
    const data = (await res.json()) as { status?: string; port?: number; sessionToken?: string };
    if (data?.status === 'ok' && data.port && data.sessionToken) {
      return {
        port: data.port,
        sessionToken: data.sessionToken,
      };
    }
  } catch (err) {
    console.debug(`[SheepGet] Discover probe failed on port ${candidatePort}:`, err);
  } finally {
    clearTimeout(timer);
  }
  return null;
}

/** 事件长连接句柄：既能主动断开，也能随时问「现在还连着吗」。 */
export interface DesktopEventLink {
  close(): void;
  isOpen(): boolean;
}

export class DesktopClient {
  private session: SessionMetadata;

  constructor(session: SessionMetadata) {
    this.session = session;
  }

  // 路径本身已经带上了协议前缀（见 internal/protocol），这里只给出回环地址。
  private get origin(): string {
    return `http://127.0.0.1:${this.session.port}`;
  }

  private get headers(): Record<string, string> {
    return {
      'Content-Type': 'application/json',
      [HeaderNames.Token]: this.session.sessionToken,
    };
  }

  async ping(timeoutMs = 1500): Promise<boolean> {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);
      const res = await fetch(`${this.origin}${Paths.Ping}`, {
        headers: this.headers,
        signal: controller.signal,
      });
      clearTimeout(timer);
      return res.ok;
    } catch {
      return false;
    }
  }

  async fetchTakeoverConfig(
    currentVersion?: number,
    timeoutMs = 2000,
  ): Promise<TakeoverConfigSync | null> {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);

      // Lightweight HEAD check first if currentVersion is provided
      if (currentVersion !== undefined && currentVersion > 0) {
        const headRes = await fetch(`${this.origin}${Paths.TakeoverConfig}`, {
          method: 'HEAD',
          headers: this.headers,
          signal: controller.signal,
        });
        const serverVersion = headRes.headers.get(HeaderNames.ConfigVersion);
        if (serverVersion && parseInt(serverVersion, 10) === currentVersion) {
          clearTimeout(timer);
          return null; // Version is unchanged
        }
      }

      // Fetch full config
      const res = await fetch(`${this.origin}${Paths.TakeoverConfig}`, {
        headers: this.headers,
        signal: controller.signal,
      });
      clearTimeout(timer);

      if (!res.ok) return null;
      return (await res.json()) as TakeoverConfigSync;
    } catch (err) {
      console.warn('[SheepGet] Failed to fetch takeover config from desktop:', err);
      return null;
    }
  }

  /**
   * 交接一次下载。失败时除了原因文案，还带上失败种类：调用方据此决定
   * 「重新发现会话后重试」还是「把下载还给浏览器」（见 lib/handover.ts）。
   * 桌面端重启会换端口，把两者混成同一个 false 会让一次本可自愈的失败
   * 变成「点了下载什么都没发生」。
   */
  async sendHandover(req: HandoverRequest, timeoutMs = 2500): Promise<HandoverResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const res = await fetch(`${this.origin}${Paths.Handover}`, {
        method: 'POST',
        headers: this.headers,
        body: JSON.stringify(req),
        signal: controller.signal,
      });
      clearTimeout(timer);

      if (res.status === 401 || res.status === 403) {
        return {
          accepted: false,
          failure: 'unauthorized',
          reason: `Session token rejected (HTTP ${res.status})`,
        };
      }
      if (!res.ok) {
        return {
          accepted: false,
          failure: 'rejected',
          reason: `Server returned HTTP ${res.status}`,
        };
      }
      return (await res.json()) as HandoverResponse;
    } catch (err) {
      clearTimeout(timer);
      const isTimeout = err instanceof DOMException && err.name === 'AbortError';
      return {
        accepted: false,
        failure: isTimeout ? 'no_response' : 'not_delivered',
        reason: isTimeout
          ? `Request timed out after ${timeoutMs}ms`
          : `Cannot reach desktop: ${String(err)}`,
      };
    }
  }

  /**
   * 拉取一份 HLS 清单的可选清晰度，供悬浮条在交接前弹菜单。请求上下文（Referer/Cookie）
   * 由调用方随 cookies/headers 一并传进来——防盗链的清单不带它只会拿到 403。
   */
  async fetchHLSVariants(
    url: string,
    credentials: { cookies?: string; headers?: Record<string, string> },
    timeoutMs = 3000,
  ): Promise<HLSVariantsResponse | null> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const res = await fetch(`${this.origin}${Paths.HLSVariants}`, {
        method: 'POST',
        headers: this.headers,
        body: JSON.stringify({ url, credentials }),
        signal: controller.signal,
      });
      clearTimeout(timer);
      if (!res.ok) return null;
      return (await res.json()) as HLSVariantsResponse;
    } catch {
      clearTimeout(timer);
      return null;
    }
  }

  /**
   * 探测一条媒体的展示信息（时长与大小），供面板在下载之前显示。
   * 探测在桌面端完成（直链走容器解析、HLS 读清单），失败返回 null，调用方按未知显示。
   */
  async fetchMediaProbe(
    req: {
      url: string;
      filename?: string;
      mimeType?: string;
      isHls?: boolean;
      totalBytes?: number;
    },
    credentials: { cookies?: string; headers?: Record<string, string> },
    timeoutMs = 12000,
  ): Promise<MediaProbeInfo | null> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const res = await fetch(`${this.origin}${Paths.MediaProbe}`, {
        method: 'POST',
        headers: this.headers,
        body: JSON.stringify({ ...req, credentials }),
        signal: controller.signal,
      });
      clearTimeout(timer);
      if (!res.ok) return null;
      return (await res.json()) as MediaProbeInfo;
    } catch {
      clearTimeout(timer);
      return null;
    }
  }

  /**
   * 订阅桌面端的事件广播。
   * `onLinkStateChange` 报告长连接的打开与关闭：桌面端进程一退出这个连接就会断，
   * 它是「桌面端已经没了」最快的一条信号，比等到下一次交接失败再发现及时得多。
   * 只有真正打开过的连接才会上报关闭——连都没连上时 HTTP 侧可能仍然可用，
   * 那不该被当成链路掉线，否则界面状态会随每次保活探测来回跳。
   */
  connectEvents(
    onConfigUpdated: (cfg: TakeoverConfigSync) => void,
    onLinkStateChange?: (open: boolean) => void,
    onServerMigrated?: (newPort: number) => void,
  ): DesktopEventLink {
    const wsUrl = `ws://127.0.0.1:${this.session.port}${Paths.Events}?${QueryParams.Token}=${encodeURIComponent(
      this.session.sessionToken,
    )}`;
    let ws: WebSocket | null = null;
    let opened = false;
    try {
      ws = new WebSocket(wsUrl);
      ws.onopen = () => {
        opened = true;
        onLinkStateChange?.(true);
      };
      ws.onclose = () => {
        if (opened) onLinkStateChange?.(false);
      };
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data as string) as {
            event: string;
            data: unknown;
          };
          if (msg.event === WsEvents.TakeoverConfigUpdated && msg.data) {
            onConfigUpdated(msg.data as TakeoverConfigSync);
          } else if (msg.event === WsEvents.ServerMigrated && msg.data) {
            const data = msg.data as { port?: number };
            if (data.port && typeof data.port === 'number') {
              onServerMigrated?.(data.port);
            }
          }
        } catch {
          // Ignore invalid message
        }
      };
      ws.onerror = () => {
        // Will close
      };
    } catch {
      // Ignore WebSocket connection error
    }

    return {
      isOpen: () => ws?.readyState === WebSocket.OPEN,
      close: () => {
        if (!ws) return;
        // 主动断开不该被当成「桌面端掉线」，先把回调摘掉再关。
        ws.onopen = null;
        ws.onclose = null;
        try {
          ws.close();
        } catch {
          // Ignore
        }
      },
    };
  }
}
