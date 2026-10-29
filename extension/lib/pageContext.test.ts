import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { createPageContextReporter, type PageContext } from './pageContext';

function harness(initial: PageContext) {
  let current = initial;
  const sent: PageContext[] = [];
  const report = createPageContextReporter(
    () => current,
    (context) => sent.push(context),
  );
  return {
    report,
    sent,
    setContext(next: PageContext) {
      current = next;
    },
  };
}

describe('createPageContextReporter', () => {
  it('第一次读取就上报当前页面上下文', () => {
    const { report, sent } = harness({ pageTitle: '视频 A', pageUrl: 'https://example.com/a' });
    report();
    assert.deepEqual(sent, [{ pageTitle: '视频 A', pageUrl: 'https://example.com/a' }]);
  });

  it('上下文没变时重复触发不会重复上报', () => {
    const { report, sent } = harness({ pageTitle: '视频 A', pageUrl: 'https://example.com/a' });
    report();
    report();
    report();
    assert.equal(sent.length, 1);
  });

  it('站内切换后 URL 或标题变化会重新上报', () => {
    const { report, sent, setContext } = harness({
      pageTitle: '视频 A',
      pageUrl: 'https://example.com/a',
    });
    report();

    setContext({ pageTitle: '视频 B', pageUrl: 'https://example.com/b' });
    report();
    assert.equal(sent.length, 2);
    assert.deepEqual(sent[1], { pageTitle: '视频 B', pageUrl: 'https://example.com/b' });

    // 只换 URL（播放列表切集但标题没变）同样要上报。
    setContext({ pageTitle: '视频 B', pageUrl: 'https://example.com/b?ep=2' });
    report();
    assert.equal(sent.length, 3);
  });
});
