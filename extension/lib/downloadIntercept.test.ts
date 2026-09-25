import { describe, it, after } from 'node:test';
import assert from 'node:assert/strict';
import type * as backgroundModule from '../entrypoints/background';
import { KEY_MASKS } from './shortcuts';
import { STORAGE_KEYS } from './storage';
import type { ExtensionMessage, TakeoverConfigSync } from './types';

const CONFIG_KEY = STORAGE_KEYS.TAKEOVER_CONFIG;

function rules(version: number, extensions: string[]): TakeoverConfigSync {
  return {
    version,
    extensions,
    excludedSites: [],
    pauseShortcut: 'Delete',
    forceShortcut: 'Insert',
  };
}

interface GlobalStubs {
  chrome?: unknown;
  defineBackground?: unknown;
  fetch?: typeof fetch;
}

const realFetch = globalThis.fetch;
const offlineFetch: typeof fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  const url =
    typeof input === 'string'
      ? input
      : input instanceof URL
        ? input.toString()
        : (input as Request).url;
  if (url.includes('127.0.0.1') || url.includes('localhost')) {
    throw new Error('桌面端没运行');
  }
  return realFetch(input, init);
}) as typeof fetch;

// 测试执行期间全局拦截本地回环请求，防止 Service Worker 异步残留重试穿透到本机运行中的桌面端
globalThis.fetch = offlineFetch;

// 判定只读这几个字段，其余由 chrome 的类型要求补齐。
const downloadItem = {
  id: 42,
  url: 'https://mirror.example.com/tool.zip',
  finalUrl: 'https://mirror.example.com/tool.zip',
  filename: 'tool.zip',
  referrer: 'https://example.com/downloads',
  mime: 'application/zip',
  totalBytes: 4096,
} as unknown as chrome.downloads.DownloadItem;

/** 判定留下的诊断日志（background.ts 的 logDownloadDecision）。 */
interface DecisionLog {
  takeover: boolean;
  reason: string;
  configVersion: number;
}

/**
 * 判定不接管时它对这次下载什么都不做，没有别的可观察出口；接管时也要知道是哪条规则说了算。
 * 所以读它留下的那条日志——那是这条链路唯一同时覆盖两种结论的痕迹。
 */
function captureDecisionLog(): {
  at(index: number): Promise<DecisionLog>;
  count(): number;
  restore(): void;
} {
  const decisions: DecisionLog[] = [];
  const waiters: { index: number; resolve: (log: DecisionLog) => void }[] = [];
  const original = console.info;

  console.info = (...args: unknown[]) => {
    if (args[0] !== '[SheepGet] download decision' || typeof args[1] !== 'string') return;
    const log = JSON.parse(args[1]) as DecisionLog;
    decisions.push(log);
    for (let i = waiters.length - 1; i >= 0; i -= 1) {
      const waiter = waiters[i];
      const ready = waiter ? decisions[waiter.index] : undefined;
      if (waiter && ready) {
        waiter.resolve(ready);
        waiters.splice(i, 1);
      }
    }
  };

  return {
    at(index) {
      const ready = decisions[index];
      if (ready) return Promise.resolve(ready);
      const pending = Promise.withResolvers<DecisionLog>();
      waiters.push({ index, resolve: pending.resolve });
      return pending.promise;
    },
    count() {
      return decisions.length;
    },
    restore() {
      console.info = original;
    },
  };
}

interface Harness {
  chrome: unknown;
  /** 入口登记的下载监听器：用例据此投递下载事件。 */
  created: Promise<(item: chrome.downloads.DownloadItem) => void>;
  /** 入口登记的消息监听器：用例据此投递内容脚本会发的那些消息。 */
  message: Promise<(msg: ExtensionMessage) => void>;
  /** 入口发起了规则读取（这次唤醒的第一件事）。 */
  configReadIssued: Promise<void>;
  /** 交接失败、下载被还给浏览器（走完接管流程的信号）。 */
  resumed: Promise<number>;
  paused: number[];
  /** 每次向页面问按键状态都记一笔。 */
  pageQueries: { tabId: number; type: string }[];
}

/** 只实现入口在这条流程里用到的那些 API，其余保持缺席。 */
function createHarness(options: {
  stored: Record<string, unknown>;
  /** 卡住首次规则读取：模拟「这次唤醒就是下载事件，存储还没读回来」。 */
  configReadGate?: Promise<void>;
  tabs?: number[];
  pageKeyMask?: number;
}): Harness {
  const created = Promise.withResolvers<(item: chrome.downloads.DownloadItem) => void>();
  const message = Promise.withResolvers<(msg: ExtensionMessage) => void>();
  const configReadIssued = Promise.withResolvers<void>();
  const resumed = Promise.withResolvers<number>();
  const paused: number[] = [];
  const pageQueries: { tabId: number; type: string }[] = [];
  const pageKeyMask = options.pageKeyMask ?? 0;
  const tabs = options.tabs ?? [7];

  const fakeChrome = {
    runtime: {
      onMessage: {
        // 假 chrome 只关心消息本身，发送者与应答回调给空实现。
        addListener: (listener: unknown) => {
          message.resolve((msg) => (listener as (msg: ExtensionMessage) => void)(msg));
        },
      },
    },
    alarms: { onAlarm: { addListener() {} }, create: async () => {} },
    tabs: {
      onActivated: { addListener() {} },
      onRemoved: { addListener() {} },
      onUpdated: { addListener() {} },
      sendMessage: async (tabId: number, message: { type: string }) => {
        pageQueries.push({ tabId, type: message.type });
        // 页面没按住键时不应答，发送方拿到的是「消息通道在应答前关闭」。
        if (pageKeyMask === 0) {
          throw new Error('The message port closed before a response was received.');
        }
        return { keyMask: pageKeyMask };
      },
      query: async () => tabs.map((id) => ({ id })),
    },
    downloads: {
      onCreated: {
        addListener: (listener: (item: chrome.downloads.DownloadItem) => void) =>
          created.resolve(listener),
      },
      pause: async (id: number) => {
        paused.push(id);
      },
      resume: async (id: number) => {
        resumed.resolve(id);
      },
      cancel: async () => {},
      erase: async () => {},
    },
    cookies: { getAll: async () => [] },
    storage: {
      local: {
        get: async (keys: string | string[]) => {
          const wanted = Array.isArray(keys) ? keys : [keys];
          const found: Record<string, unknown> = {};
          if (wanted.includes(CONFIG_KEY)) {
            configReadIssued.resolve();
            await options.configReadGate;
          }
          for (const key of wanted) {
            if (key in options.stored) found[key] = options.stored[key];
          }
          return found;
        },
        set: async () => {},
        remove: async () => {},
      },
      onChanged: { addListener() {} },
    },
    webRequest: { onHeadersReceived: { addListener() {} } },
    action: { setBadgeText: async () => {}, setBadgeBackgroundColor: async () => {} },
  };

  return {
    chrome: fakeChrome,
    created: created.promise,
    message: message.promise,
    configReadIssued: configReadIssued.promise,
    resumed: resumed.promise,
    paused,
    pageQueries,
  };
}

let instances = 0;

/**
 * 启动真实入口。它在求值时就装监听器，所以必须先备好假浏览器，只能动态导入；
 * 带查询串的导入让入口重新求值，于是每个用例都有自己的一份 Service Worker 状态。
 */
async function startServiceWorker(fakeChrome: unknown): Promise<() => void> {
  const globals = globalThis as unknown as GlobalStubs;
  const originalChrome = globals.chrome;
  const originalDefine = globals.defineBackground;
  globals.chrome = fakeChrome;
  globals.defineBackground = (main: () => void) => main;
  globals.fetch = offlineFetch;

  const definition = (await import(
    `../entrypoints/background?case=${++instances}`
  )) as typeof backgroundModule;
  // defineBackground 的桩把入口函数原样交回来，这里手动启动这次唤醒。
  const start = definition.default as unknown as () => void;
  start();

  return () => {
    globals.chrome = originalChrome;
    globals.defineBackground = originalDefine;
    // 单个用例结束时仍保持回环阻断，防止未结算的异步重试穿透到本地桌面端
    globals.fetch = offlineFetch;
  };
}

/**
 * 排空微任务：判定链此刻只可能卡在存储读取上，排空之后仍无动静才算它没有越过就绪。
 * 用微任务而不是等一小段时间，是为了不把结论绑在机器快慢上。
 */
async function drainMicrotasks(rounds = 20) {
  for (let i = 0; i < rounds; i += 1) await Promise.resolve();
}

/** 超时只把「迟迟没有发生」变成一条读得懂的失败：等待本身始终靠真实信号，不做定时轮询。 */
async function withTimeout<T>(signal: Promise<T>, label: string, ms = 2000): Promise<T> {
  const timeout = Promise.withResolvers<T>();
  const timer = setTimeout(() => timeout.reject(new Error(`等待超时：${label}`)), ms);
  try {
    return await Promise.race([signal, timeout.promise]);
  } finally {
    clearTimeout(timer);
  }
}

/**
 * 这三个用例共用一份模块图：带查询串的导入只让入口重新求值，它导入的依赖（规则状态）
 * 仍是同一份，因此**只有第一条用例能卡住首次规则读取**——它读回来的规则，后面的用例直接用。
 * 后面的用例不依赖具体清单：暂停键与强制键都排在「后缀是否命中」之前，任何清单下都成立。
 */
describe('冷启动的下载事件', () => {
  after(() => {
    globalThis.fetch = realFetch;
  });

  it('规则没读回来之前不判定；读回来之后按已恢复的清单接管', async () => {
    const configReadGate = Promise.withResolvers<void>();
    const browser = createHarness({
      stored: { [CONFIG_KEY]: rules(7, ['zip']) },
      configReadGate: configReadGate.promise,
    });
    const log = captureDecisionLog();
    const restore = await startServiceWorker(browser.chrome);

    try {
      await browser.configReadIssued;
      const intercept = await browser.created;
      intercept(downloadItem);
      await drainMicrotasks();
      assert.deepEqual(browser.paused, [], '规则未就绪时不得给出「不接管」的结论');
      assert.equal(log.count(), 0, '规则未就绪时判定还没发生');

      configReadGate.resolve();
      const decision = await withTimeout(log.at(0), '规则就绪后要给出结论');
      assert.equal(decision.takeover, true);
      assert.equal(decision.reason, 'extension_matched');
      assert.equal(decision.configVersion, 7, '判定用的是已恢复的规则');
      assert.equal(
        await withTimeout(browser.resumed, '按已恢复的规则接管后，把下载还给离线的桌面端'),
        downloadItem.id,
      );
      assert.deepEqual(browser.paused, [downloadItem.id]);
    } finally {
      restore();
      log.restore();
    }
  });

  it('按住暂停键点下载链接、下载晚几秒才开始时，仍然不接管', async () => {
    const browser = createHarness({
      stored: { [CONFIG_KEY]: rules(7, ['zip']) },
      pageKeyMask: KEY_MASKS.Delete,
    });
    const log = captureDecisionLog();
    const restore = await startServiceWorker(browser.chrome);

    try {
      // 按住 Delete 点下载链接：点击意图会记下来，但它 3 秒后就过期；真正还知道
      // 「Delete 正按着」的只有页面，所以判定必须现问一次（这正是缓存答案会翻车的地方）。
      const send = await browser.message;
      send({
        type: 'SHORTCUT_CLICKED',
        keyMask: KEY_MASKS.Delete,
        url: downloadItem.finalUrl,
        timestamp: Date.now() - 5000,
      });

      const intercept = await browser.created;
      intercept(downloadItem);

      const decision = await withTimeout(log.at(0), '按住的暂停键要影响这次判定');
      assert.equal(decision.takeover, false);
      assert.equal(
        decision.reason,
        'pause_shortcut_active',
        '过期的点击意图之后，按住的 Delete 仍要生效',
      );
      assert.deepEqual(browser.paused, []);
      assert.deepEqual(browser.pageQueries, [{ tabId: 7, type: 'QUERY_KEY_STATE' }]);
    } finally {
      restore();
      log.restore();
    }
  });

  it('页面按住的强制键在冷启动后仍然生效，且每次判定都现问一次页面', async () => {
    const browser = createHarness({
      stored: { [CONFIG_KEY]: rules(7, ['zip']) },
      pageKeyMask: KEY_MASKS.Insert,
    });
    const log = captureDecisionLog();
    const restore = await startServiceWorker(browser.chrome);

    try {
      const intercept = await browser.created;
      intercept(downloadItem);

      const first = await withTimeout(log.at(0), '按住的强制键要影响这次判定');
      assert.equal(first.takeover, true);
      assert.equal(first.reason, 'force_shortcut_active', '页面按住的 Insert 必须传到判定里');
      assert.equal(
        await withTimeout(browser.resumed, '强制接管后把下载还给离线的桌面端'),
        downloadItem.id,
      );

      intercept(downloadItem);
      const second = await withTimeout(log.at(1), '第二次下载照样要判定');
      assert.equal(second.reason, 'force_shortcut_active');
      assert.equal(browser.pageQueries.length, 2, '每次判定都要现问一次页面：上一次的答案会过期');
    } finally {
      restore();
      log.restore();
    }
  });
});
