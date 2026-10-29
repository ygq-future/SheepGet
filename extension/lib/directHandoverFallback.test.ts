import { after, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import type * as backgroundModule from '../entrypoints/background';

const CONFIG_KEY = 'sheepget_takeover_config';
const TARGET_KEY = 'sheepget_download_target_config';

interface DownloadItem {
  id: number;
  url: string;
  finalUrl?: string;
  filename?: string;
  mime?: string;
  state?: string;
  startTime?: string;
  referrer?: string;
  totalBytes?: number;
}

interface Harness {
  chrome: unknown;
  cancelled: number[];
  paused: number[];
  downloaded: string[];
  resumed: Promise<number>;
  send(message: unknown, sender: unknown): Promise<unknown>;
  emitDownload(item: DownloadItem): void;
}

/**
 * 离线场景的最小假浏览器：discover 永远失败，交接返回 not_delivered。
 * 这里关心的是「交接失败之后，回退下载还会不会被 onCreated 取消」。
 */
function makeHarness(): Harness {
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
  const cancelled: number[] = [];
  const paused: number[] = [];
  const downloaded: string[] = [];
  const { promise: resumed, resolve: resolveResumed } = Promise.withResolvers<number>();
  let createdListener: ((item: DownloadItem) => void) | null = null;
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
      onCreated: {
        addListener: (listener: (item: DownloadItem) => void) => {
          createdListener = listener;
        },
      },
      onChanged: { addListener() {} },
      cancel: async (id: number) => {
        cancelled.push(id);
      },
      erase: async () => {},
      download: (options: { url: string }, callback?: (id: number) => void) => {
        downloaded.push(options.url);
        callback?.(99);
      },
      pause: async (id: number) => {
        paused.push(id);
      },
      resume: async (id: number) => {
        resolveResumed(id);
      },
      search: async () => [],
    },
    tabs: {
      onActivated: { addListener() {} },
      onRemoved: { addListener() {} },
      onUpdated: { addListener() {} },
      sendMessage: async () => {
        throw new Error('no content script');
      },
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
    cookies: { getAll: async () => [] },
  };

  return {
    chrome,
    cancelled,
    paused,
    downloaded,
    resumed,
    send(message: unknown, sender: unknown): Promise<unknown> {
      const { promise, resolve } = Promise.withResolvers<unknown>();
      const handled = messageListener?.(message, sender, resolve);
      if (handled !== true) resolve(undefined);
      return promise;
    },
    emitDownload(item: DownloadItem) {
      createdListener?.(item);
    },
  };
}

const realFetch = globalThis.fetch;
const realChrome = (globalThis as { chrome?: unknown }).chrome;
const realDefineBackground = (globalThis as { defineBackground?: unknown }).defineBackground;

let instances = 0;

async function startServiceWorker(harness: Harness): Promise<void> {
  const globals = globalThis as unknown as {
    chrome?: unknown;
    defineBackground?: (main: () => void) => void;
    fetch: typeof fetch;
  };
  globals.chrome = harness.chrome;
  globals.defineBackground = (main: () => void) => main;
  globals.fetch = (async () => {
    throw new Error('desktop offline');
  }) as typeof fetch;

  // 入口在求值时就安装监听器；必须先备好假浏览器，只能动态导入（模块加载边界）。
  const definition = (await import(
    `../entrypoints/background?fallback=${++instances}`
  )) as typeof backgroundModule;
  (definition.default as unknown as () => void)();
}

async function drainMicrotasks(rounds = 50) {
  for (let i = 0; i < rounds; i += 1) await Promise.resolve();
}

after(() => {
  const globals = globalThis as unknown as {
    chrome?: unknown;
    defineBackground?: unknown;
    fetch: typeof fetch;
  };
  globals.fetch = realFetch;
  globals.chrome = realChrome;
  globals.defineBackground = realDefineBackground;
});

/** 交接失败的形状只关心失败种类；其它字段不参与判定。 */
function failureOf(response: unknown): string | undefined {
  if (!response || typeof response !== 'object' || !('failure' in response)) return undefined;
  return typeof response.failure === 'string' ? response.failure : undefined;
}

const URL = 'https://mirror.example.com/tool.zip';

describe('直接点击交接失败后的回退', () => {
  it('交接返回 not_delivered 后，同一 URL 的回退下载不会被取消', async () => {
    const harness = makeHarness();
    await startServiceWorker(harness);

    const response = await harness.send(
      {
        type: 'DIRECT_CLICK_HANDOVER',
        url: URL,
        pageUrl: 'https://example.com/page',
        pageTitle: 'page',
        referrer: 'https://example.com/page',
      },
      { tab: { id: 7 }, frameId: 0 },
    );
    assert.equal(failureOf(response), 'not_delivered');

    // 内容脚本回退后，浏览器为同一 URL 创建下载；它必须走正常的 onCreated 路径
    // （pause → 交接失败 → resume），而不是被防重表取消。
    harness.emitDownload({
      id: 42,
      url: URL,
      finalUrl: URL,
      filename: 'tool.zip',
      mime: 'application/zip',
      state: 'in_progress',
      startTime: new Date().toISOString(),
      referrer: 'https://example.com/page',
      totalBytes: 4096,
    });

    assert.equal(await harness.resumed, 42, '离线时下载必须还给浏览器');
    assert.deepEqual(harness.cancelled, [], '回退下载不能被取消');
    assert.deepEqual(harness.paused, [42], '回退下载仍按普通拦截路径先挂起再恢复');
  });

  it('TRIGGER_BROWSER_DOWNLOAD 发起的下载不会被 onCreated 二次拦截', async () => {
    const harness = makeHarness();
    await startServiceWorker(harness);

    const response = await harness.send(
      { type: 'TRIGGER_BROWSER_DOWNLOAD', url: URL, filename: 'tool.zip' },
      { tab: { id: 7 }, frameId: 0 },
    );
    assert.deepEqual(response, { accepted: true, queueItemId: '99' });
    assert.deepEqual(harness.downloaded, [URL]);

    harness.emitDownload({
      id: 43,
      url: URL,
      finalUrl: URL,
      filename: 'tool.zip',
      mime: 'application/zip',
      state: 'in_progress',
      startTime: new Date().toISOString(),
    });
    await drainMicrotasks();

    assert.deepEqual(harness.paused, [], '扩展自己发起的下载不进入接管判定');
    assert.deepEqual(harness.cancelled, []);
  });
});
