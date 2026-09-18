import type {
  HandoverRequest,
  HandoverResponse,
  SessionMetadata,
  TakeoverConfigSync,
} from './types';

export class DesktopClient {
  private session: SessionMetadata;

  constructor(session: SessionMetadata) {
    this.session = session;
  }

  private get baseUrl(): string {
    return `http://127.0.0.1:${this.session.port}/api/v1`;
  }

  private get headers(): Record<string, string> {
    return {
      'Content-Type': 'application/json',
      'X-SheepGet-Token': this.session.sessionToken,
    };
  }

  async ping(timeoutMs = 1500): Promise<boolean> {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);
      const res = await fetch(`${this.baseUrl}/ping`, {
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
        const headRes = await fetch(`${this.baseUrl}/config/takeover`, {
          method: 'HEAD',
          headers: this.headers,
          signal: controller.signal,
        });
        const serverVersion = headRes.headers.get('X-Config-Version');
        if (serverVersion && parseInt(serverVersion, 10) === currentVersion) {
          clearTimeout(timer);
          return null; // Version is unchanged
        }
      }

      // Fetch full config
      const res = await fetch(`${this.baseUrl}/config/takeover`, {
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

  async sendHandover(req: HandoverRequest, timeoutMs = 2500): Promise<HandoverResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const res = await fetch(`${this.baseUrl}/handover`, {
        method: 'POST',
        headers: this.headers,
        body: JSON.stringify(req),
        signal: controller.signal,
      });
      clearTimeout(timer);

      if (!res.ok) {
        return {
          accepted: false,
          reason: `Server returned HTTP ${res.status}`,
        };
      }
      return (await res.json()) as HandoverResponse;
    } catch (err) {
      clearTimeout(timer);
      const isTimeout = err instanceof DOMException && err.name === 'AbortError';
      return {
        accepted: false,
        reason: isTimeout ? 'Request timed out after 2.5s' : String(err),
      };
    }
  }

  connectEvents(onConfigUpdated: (cfg: TakeoverConfigSync) => void): () => void {
    const wsUrl = `ws://127.0.0.1:${this.session.port}/api/v1/events?token=${encodeURIComponent(this.session.sessionToken)}`;
    let ws: WebSocket | null = null;
    try {
      ws = new WebSocket(wsUrl);
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data as string) as {
            event: string;
            data: unknown;
          };
          if (msg.event === 'takeover_config_updated' && msg.data) {
            onConfigUpdated(msg.data as TakeoverConfigSync);
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

    return () => {
      closed = true;
      if (ws) {
        try {
          ws.close();
        } catch {
          // Ignore
        }
      }
    };
  }
}
