import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { LINK_VERIFY_TTL_MS, planHandoverFailure, shouldReverifyLink } from './handover';

describe('planHandoverFailure', () => {
  it('连接没能建立时重新发现会话并重试：请求一定没到达桌面端', () => {
    assert.equal(planHandoverFailure('not_delivered'), 'retry_after_rediscover');
  });

  it('令牌失效时重新发现会话并重试：桌面端已经换了会话', () => {
    assert.equal(planHandoverFailure('unauthorized'), 'retry_after_rediscover');
  });

  it('超时不重试：请求可能已经被受理，重试会造出两个任务', () => {
    assert.equal(planHandoverFailure('no_response'), 'give_back_to_browser');
  });

  it('桌面端明确拒绝时不重试，直接还给浏览器', () => {
    assert.equal(planHandoverFailure('rejected'), 'give_back_to_browser');
  });
});

describe('shouldReverifyLink', () => {
  const now = 1_000_000;

  it('离线时永远要重新验证', () => {
    assert.equal(shouldReverifyLink({ online: false, lastVerifiedAt: null }, now, 30_000), true);
    assert.equal(shouldReverifyLink({ online: false, lastVerifiedAt: now }, now, 30_000), true);
  });

  it('在线但从未验证过也要验证，不能凭「存过会话」当作还连着', () => {
    assert.equal(shouldReverifyLink({ online: true, lastVerifiedAt: null }, now, 30_000), true);
  });

  it('在线且验证还在有效期内时不重复验证，避免每次下载都白跑一次 ping', () => {
    assert.equal(
      shouldReverifyLink({ online: true, lastVerifiedAt: now - 1_000 }, now, LINK_VERIFY_TTL_MS),
      false,
    );
  });

  it('验证结果超过有效期即重新验证，保证陈旧状态有上界', () => {
    assert.equal(shouldReverifyLink({ online: true, lastVerifiedAt: now - 1_000 }, now, 999), true);
  });

  it('恰好到有效期边界就重新验证', () => {
    assert.equal(
      shouldReverifyLink({ online: true, lastVerifiedAt: now - 30_000 }, now, 30_000),
      true,
    );
  });
});
