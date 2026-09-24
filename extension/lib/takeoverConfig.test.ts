import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_TAKEOVER_CONFIG, STORAGE_KEYS } from './storage';
import type * as takeoverConfigModule from './takeoverConfig';
import type { TakeoverConfigSync } from './types';

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

interface LocalStorage {
  get(keys: string | string[]): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
}

interface GlobalStubs {
  chrome?: unknown;
}

let instances = 0;

/**
 * 模块把「已就绪」与读回的规则记在模块级变量里，静态导入会让所有用例共用同一份状态；
 * 带查询串的动态导入按完整说明符取一份新实例，这里要验证的正是这条加载边界。
 */
async function freshModule(storage: Partial<LocalStorage>) {
  const globals = globalThis as unknown as GlobalStubs;
  globals.chrome = {
    storage: { local: { get: async () => ({}), set: async () => {}, ...storage } },
  };
  const specifier = `./takeoverConfig?case=${++instances}`;
  return (await import(specifier)) as typeof takeoverConfigModule;
}

describe('接管规则的就绪接缝', () => {
  it('存储读回来之前不给规则；读回来之后给出已恢复的规则，且只读一次', async () => {
    const readGate = Promise.withResolvers<void>();
    const readIssued = Promise.withResolvers<void>();
    let reads = 0;
    const mod = await freshModule({
      get: async () => {
        reads += 1;
        readIssued.resolve();
        await readGate.promise;
        return { [CONFIG_KEY]: rules(7, ['zip']) };
      },
    });

    let settled = false;
    const ready = mod.readyConfig().then((config) => {
      settled = true;
      return config;
    });
    await readIssued.promise;
    assert.equal(reads, 1);
    assert.equal(settled, false, '存储还没读回来，判定方只能等，不能把模块初值当规则');

    readGate.resolve();
    assert.deepEqual(await ready, rules(7, ['zip']));
    assert.deepEqual(await mod.readyConfig(), rules(7, ['zip']));
    assert.equal(reads, 1, '整个 Service Worker 生命周期只读一次存储');

    // 就绪是终态：此后落进来的规则直接生效，不再回头读存储。
    await mod.applyConfig(rules(8, ['rar']));
    assert.deepEqual(await mod.readyConfig(), rules(8, ['rar']));
    assert.equal(reads, 1);
  });

  it('读取期间落进来的规则不会被读回的旧值覆盖', async () => {
    const readGate = Promise.withResolvers<void>();
    const readIssued = Promise.withResolvers<void>();
    let persisted: Record<string, unknown> = {};
    const mod = await freshModule({
      get: async () => {
        readIssued.resolve();
        await readGate.promise;
        return { [CONFIG_KEY]: rules(1, ['zip']) };
      },
      set: async (items) => {
        persisted = items;
      },
    });

    const ready = mod.readyConfig();
    await readIssued.promise;
    await mod.applyConfig(rules(2, ['7z'])); // 读取还在途中，桌面端先把新规则广播过来
    readGate.resolve();

    assert.deepEqual(await ready, rules(2, ['7z']), '晚到的旧值不能盖掉已经落进来的规则');
    assert.deepEqual(
      persisted,
      { [CONFIG_KEY]: rules(2, ['7z']) },
      '下发的规则要落盘，下次冷启动直接可用',
    );
  });

  it('存储读不出来时就绪仍然成立：按空清单判定，而不是把判定挂住', async () => {
    const mod = await freshModule({
      get: async () => {
        throw new Error('storage unavailable');
      },
    });

    const config = await mod.readyConfig();
    assert.deepEqual(config, DEFAULT_TAKEOVER_CONFIG);
    assert.deepEqual(config.extensions, [], '读不到规则等于不接管，下载留在浏览器里正常完成');
  });
});
