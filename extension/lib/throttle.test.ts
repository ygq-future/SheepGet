import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { createThrottledRunner } from './throttle';

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('createThrottledRunner', () => {
  it('第一次触发立即执行，窗口内的其余触发合并成一次尾随执行', async () => {
    let runs = 0;
    const runner = createThrottledRunner(() => {
      runs += 1;
    }, 40);

    for (let i = 0; i < 50; i += 1) {
      runner.trigger();
    }
    assert.equal(runs, 1, '第一次触发应当立即执行');

    await sleep(80);
    assert.equal(runs, 2, '窗口期间的触发应当合并成一次尾随执行');
    runner.cancel();
  });

  it('持续变更不会饿死执行：每个窗口最多一次', async () => {
    let runs = 0;
    const runner = createThrottledRunner(() => {
      runs += 1;
    }, 30);

    const deadline = Date.now() + 150;
    while (Date.now() < deadline) {
      runner.trigger();
      await sleep(1);
    }
    runner.cancel();

    // 150ms / 30ms ≈ 5 次，允许调度抖动，但必须明显少于触发次数。
    assert.ok(runs >= 3, `持续变更期间至少执行几次，实际 ${runs}`);
    assert.ok(runs <= 8, `执行次数应当被窗口限制住，实际 ${runs}`);
  });

  it('cancel 丢弃尚未执行的尾随调用', async () => {
    let runs = 0;
    const runner = createThrottledRunner(() => {
      runs += 1;
    }, 40);

    runner.trigger();
    runner.trigger();
    runner.cancel();

    await sleep(80);
    assert.equal(runs, 1);
  });
});
