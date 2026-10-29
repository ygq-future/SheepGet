import { after, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import type * as backgroundModule from '../entrypoints/background';

const CONFIG_KEY = 'sheepget_takeover_config';
const TARGET_KEY = 'sheepget_download_target_config';

interface CapturedHandover {
  url: string;
  body: string;
}

interface Harness {
  chrome: unknown;
  handovers: CapturedHandover[];
  fetch: typeof fetch;
  send(message: unknown, sender: unknown): Promise<unknown>;
}

interface HarnessOptions {
  cookies: { name: string; value: string }[];
}

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  headers.set('Content-Type', 'application/json');
  return new Response(JSON.stringify(body), { ...init, headers });
}

/** 只实现这条链路真正会用到的浏览器 API；Cookie 的返回值由用例控制。 */
function createFetchStub(handovers: CapturedHandover[]): typeof fetch {
  return async (input, init) => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;

    if (url.endsWith('/api/v1/discover')) {
      return jsonResponse({ status: 'ok', port: 9248, sessionToken: 'test-token' });
    }
    if (url.endsWith('/api/v1/ping')) {
      return jsonResponse({});
    }
    if (url.endsWith('/api/v1/config/takeover')) {
      if (init?.method === 'HEAD') {
        return new Response(null, { status: 200, headers: { 'X-Config-Version': '9' } });
      }
      return jsonResponse({
        version: 9,
        extensions: ['zip'],
        excludedSites: [],
        pauseShortcut: 'Delete',
        forceShortcut: 'Insert',
      });
    }
    if (url.endsWith('/api/v1/handover')) {
      handovers.push({ url, body: String(init?.body ?? '') });
      return jsonResponse({ accepted: true, queueItemId: 'queue-item-1' });
    }
    return new Response('not found', { status: 404 });
  };
}

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readyState = FakeWebSocket.OPEN;
  onopen: ((event: unknown) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;

  constructor(readonly url: string) {}

  close() {
    this.readyState = FakeWebSocket.CLOSED;
  }
}

function makeHarness(options: HarnessOptions): Harness {
  const stored: Record<string, unknown> = {
    [CONFIG_KEY]: {
      version: 9,
      extensions: ['zip'],
      excludedSites: [],
      pauseShortcut: 'Delete',
      forceShortcut: 'Insert',
    },
    [TARGET_KEY]: { type: 'sheepget', aria2: { rpcUrl: '', secret: '' }, webhook: { url: '' } },
  };
  const handovers: CapturedHandover[] = [];
  let messageListener:
    ((msg: unknown, sender: unknown, sendResponse: (r: unknown) => void) => unknown) | null = null;

  const chrome = {
    runtime: {
      id: 'test-extension',
      lastError: undefined,
      onMessage: {
        addListener: (listener: typeof messageListener) => {
          messageListener = listener;
        },
      },
      onInstalled: { addListener() {} },
      onStartup: { addListener() {} },
      sendMessage: async () => undefined,
    },
    storage: {
      local: {
        get: async (keys: string | string[]) => {
          const wanted = Array.isArray(keys) ? keys : [keys];
          const result: Record<string, unknown> = {};
          for (const key of wanted) {
            if (key in stored) result[key] = stored[key];
          }
          return result;
        },
        set: async (values: Record<string, unknown>) => {
          Object.assign(stored, values);
        },
        remove: async () => {},
      },
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
      onChanged: { addListener() {} },
    },
    downloads: {
      onCreated: { addListener() {} },
      onChanged: { addListener() {} },
      cancel: async () => {},
      erase: async () => {},
      download: async () => 1,
      pause: async () => {},
      resume: async () => {},
      search: async () => [],
    },
    tabs: {
      onActivated: { addListener() {} },
      onRemoved: { addListener() {} },
      onUpdated: { addListener() {} },
      sendMessage: async () => undefined,
      query: async () => [],
      get: async (id: number) => ({ id, url: 'https://example.com/page' }),
    },
    windows: { onRemoved: { addListener() {} }, getCurrent: async () => ({ id: 1 }) },
    webNavigation: {
      onCommitted: { addListener() {} },
      onHistoryStateUpdated: { addListener() {} },
      getAllFrames: async () => [],
    },
    webRequest: {
      onHeadersReceived: { addListener() {} },
      onBeforeRequest: { addListener() {} },
      onSendHeaders: { addListener() {} },
    },
    action: {
      setBadgeText: async () => {},
      setBadgeBackgroundColor: async () => {},
      onClicked: { addListener() {} },
    },
    alarms: { onAlarm: { addListener() {} }, create: async () => {}, clear: async () => {} },
    cookies: { getAll: async () => options.cookies },
  };

  return {
    chrome,
    handovers,
    fetch: createFetchStub(handovers),
    send(message: unknown, sender: unknown): Promise<unknown> {
      const { promise, resolve } = Promise.withResolvers<unknown>();
      const handled = messageListener?.(message, sender, resolve);
      if (handled !== true) resolve(undefined);
      return promise;
    },
  };
}

const realFetch = globalThis.fetch;
const realWebSocket = globalThis.WebSocket;
const realChrome = (globalThis as { chrome?: unknown }).chrome;
const realDefineBackground = (globalThis as { defineBackground?: unknown }).defineBackground;

let instances = 0;

async function startServiceWorker(harness: Harness): Promise<void> {
  const globals = globalThis as unknown as {
    chrome?: unknown;
    defineBackground?: (main: () => void) => void;
    fetch: typeof fetch;
    WebSocket: typeof WebSocket;
  };
  globals.chrome = harness.chrome;
  globals.defineBackground = (main: () => void) => main;
  globals.fetch = harness.fetch;
  globals.WebSocket = FakeWebSocket as unknown as typeof WebSocket;

  // 入口在求值时就安装监听器；必须先备好假浏览器，只能动态导入（模块加载边界）。
  const definition = (await import(
    `../entrypoints/background?cookies=${++instances}`
  )) as typeof backgroundModule;
  (definition.default as unknown as () => void)();
}

after(() => {
  const globals = globalThis as unknown as {
    chrome?: unknown;
    defineBackground?: unknown;
    fetch: typeof fetch;
    WebSocket: typeof WebSocket;
  };
  globals.fetch = realFetch;
  globals.WebSocket = realWebSocket;
  globals.chrome = realChrome;
  globals.defineBackground = realDefineBackground;
});

/** 从交接请求体里读 Cookie；只接受字符串，形状不对就当没带。 */
function handoverCookies(body: unknown): string | undefined {
  if (!body || typeof body !== 'object' || !('credentials' in body)) return undefined;
  const credentials = body.credentials;
  if (!credentials || typeof credentials !== 'object' || !('cookies' in credentials)) {
    return undefined;
  }
  const cookies = credentials.cookies;
  return typeof cookies === 'string' ? cookies : undefined;
}

describe('扩展的 Cookie 转发', () => {
  it('直接点击交接时把浏览器 Cookie 放进请求上下文', async () => {
    const harness = makeHarness({ cookies: [{ name: 'SESSION_ID', value: 'abc123' }] });
    await startServiceWorker(harness);

    const response = await harness.send(
      {
        type: 'DIRECT_CLICK_HANDOVER',
        url: 'https://example.com/tool.zip',
        pageUrl: 'https://example.com/page',
        pageTitle: 'page',
        referrer: 'https://example.com/page',
      },
      { tab: { id: 7 }, frameId: 0 },
    );

    assert.deepEqual(response, { accepted: true, queueItemId: 'queue-item-1' });
    assert.equal(harness.handovers.length, 1, '交接请求应该发出去一次');
    const [handover] = harness.handovers;
    assert.ok(handover, '交接请求应该被记录');
    assert.equal(handoverCookies(JSON.parse(handover.body)), 'SESSION_ID=abc123');
  });
});
