import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { watchDesktopStatus } from './liveStatus';
import type { DesktopStatus } from './types';

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

const online: DesktopStatus = { online: true, port: 61011, lastVerifiedAt: 1, reason: null };

describe('watchDesktopStatus', () => {
  it('面板开着的时候反复确认状态，并把结果写到界面', async () => {
    let probeCount = 0;
    const seen: DesktopStatus[] = [];

    const watch = watchDesktopStatus({
      intervalMs: 10,
      probe: async () => {
        probeCount += 1;
        // 第一次查询时桌面端已经退出，状态在面板开着的过程中才变成离线。
        return probeCount >= 2
          ? { ...online, online: false, reason: '未发现运行中的桌面端' }
          : online;
      },
      onStatus: (status) => {
        if (status) seen.push(status);
      },
    });

    await sleep(60);
    watch.stop();

    assert.ok(
      probeCount >= 2,
      `expected repeated probes while the panel is open, got ${probeCount}`,
    );
    assert.equal(seen.at(-1)?.online, false, '界面必须收到打开之后才发生的状态变化');
  });

  it('stop 之后不再查询', async () => {
    let probeCount = 0;
    const watch = watchDesktopStatus({
      intervalMs: 10,
      probe: async () => {
        probeCount += 1;
        return online;
      },
      onStatus: () => {},
    });

    await sleep(35);
    watch.stop();
    const afterStop = probeCount;

    await sleep(40);
    assert.equal(probeCount, afterStop);
  });

  it('stop 之后，已经在途的那次查询不再写回界面', async () => {
    const pending: ((status: DesktopStatus) => void)[] = [];
    let written = 0;

    const watch = watchDesktopStatus({
      intervalMs: 5,
      probe: () =>
        new Promise<DesktopStatus>((resolve) => {
          pending.push(resolve);
        }),
      onStatus: () => {
        written += 1;
      },
    });

    await sleep(20);
    assert.ok(pending.length > 0, 'expected the first probe to be in flight');
    watch.stop();

    pending[0]?.({ ...online, online: false });
    await sleep(10);

    assert.equal(written, 0);
  });
});
