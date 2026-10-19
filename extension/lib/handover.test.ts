import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  LINK_VERIFY_TTL_MS,
  buildCurlCommand,
  planHandoverFailure,
  probeAria2Liveness,
  sendAria2Handover,
  sendWebhookHandover,
  shouldReverifyLink,
} from './handover';
import type { HandoverRequest } from './types';

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

describe('buildCurlCommand', () => {
  it('正确生成带文件名、Cookie 与 Headers 的 cURL 命令', () => {
    const req: HandoverRequest = {
      sourceType: 'media_bar',
      url: 'https://example.com/video.mp4',
      filenameSuggestion: 'my video.mp4',
      pageContext: { pageUrl: 'https://example.com' },
      credentials: {
        cookies: 'sid=123',
        headers: {
          Referer: 'https://example.com/watch',
          'User-Agent': 'TestUA/1.0',
        },
      },
    };
    const cmd = buildCurlCommand(req);
    assert.match(cmd, /^curl -L -o "my video\.mp4"/);
    assert.match(cmd, /-H "Referer: https:\/\/example\.com\/watch"/);
    assert.match(cmd, /-H "User-Agent: TestUA\/1\.0"/);
    assert.match(cmd, /-H "Cookie: sid=123"/);
    assert.match(cmd, /"https:\/\/example\.com\/video\.mp4"$/);
  });
});

describe('sendAria2Handover', () => {
  it('向 Aria2 RPC 正确构造 JSON-RPC 2.0 请求体并处理成功响应', async () => {
    let sentBody: any;
    const mockFetch: typeof fetch = async (_url, init) => {
      sentBody = JSON.parse(init?.body as string);
      return new Response(JSON.stringify({ jsonrpc: '2.0', id: '1', result: 'aria2_gid_123' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };

    const req: HandoverRequest = {
      sourceType: 'resource_list',
      url: 'https://example.com/file.zip',
      filenameSuggestion: 'file.zip',
      pageContext: { pageUrl: 'https://example.com' },
      credentials: {
        cookies: 'auth=token',
        headers: { Referer: 'https://example.com' },
      },
    };

    const resp = await sendAria2Handover(
      req,
      { rpcUrl: 'http://127.0.0.1:6800/jsonrpc', secret: 'mysecret', dir: '/downloads' },
      mockFetch,
    );

    assert.equal(resp.accepted, true);
    assert.equal(resp.queueItemId, 'aria2_gid_123');
    assert.equal(sentBody.method, 'aria2.addUri');
    assert.equal(sentBody.params[0], 'token:mysecret');
    assert.deepEqual(sentBody.params[1], ['https://example.com/file.zip']);
    assert.equal(sentBody.params[2].out, 'file.zip');
    assert.equal(sentBody.params[2].dir, '/downloads');
    assert.ok(sentBody.params[2].header.includes('Cookie: auth=token'));
  });

  it('Aria2 RPC 报错时返回 rejected 状态', async () => {
    const mockFetch: typeof fetch = async () => {
      return new Response(
        JSON.stringify({ jsonrpc: '2.0', id: '1', error: { message: 'Unauthorized' } }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    };

    const req: HandoverRequest = {
      sourceType: 'resource_list',
      url: 'https://example.com/file.zip',
      pageContext: { pageUrl: 'https://example.com' },
    };

    const resp = await sendAria2Handover(
      req,
      { rpcUrl: 'http://127.0.0.1:6800/jsonrpc' },
      mockFetch,
    );
    assert.equal(resp.accepted, false);
    assert.equal(resp.failure, 'rejected');
    assert.equal(resp.reason, 'Unauthorized');
  });
});

describe('sendWebhookHandover', () => {
  it('向通用 Webhook 正确投递 HandoverRequest JSON 数据', async () => {
    let sentHeaders: any;
    let sentBody: any;
    const mockFetch: typeof fetch = async (_url, init) => {
      sentHeaders = init?.headers;
      sentBody = JSON.parse(init?.body as string);
      return new Response(JSON.stringify({ accepted: true, queueItemId: 'job_456' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    };

    const req: HandoverRequest = {
      sourceType: 'media_bar',
      url: 'https://example.com/video.mp4',
      filenameSuggestion: 'video.mp4',
      pageContext: { pageUrl: 'https://example.com' },
    };

    const resp = await sendWebhookHandover(
      req,
      { url: 'https://my-webhook.test/download', token: 'secret-token' },
      mockFetch,
    );

    assert.equal(resp.accepted, true);
    assert.equal(resp.queueItemId, 'job_456');
    assert.equal(sentHeaders['Authorization'], 'Bearer secret-token');
    assert.equal(sentBody.url, 'https://example.com/video.mp4');
  });

  it('未配置 URL 时直接返回未配置错误', async () => {
    const req: HandoverRequest = {
      sourceType: 'media_bar',
      url: 'https://example.com/video.mp4',
      pageContext: { pageUrl: 'https://example.com' },
    };
    const resp = await sendWebhookHandover(req, { url: '' });
    assert.equal(resp.accepted, false);
    assert.match(resp.reason || '', /未配置/);
  });
});

describe('probeAria2Liveness', () => {
  it('在线返回 version 时返回 true', async () => {
    const mockFetch: typeof fetch = async () => {
      return new Response(
        JSON.stringify({ jsonrpc: '2.0', id: 'probe', result: { version: '1.36.0' } }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    };
    const alive = await probeAria2Liveness({ rpcUrl: 'http://127.0.0.1:6800/jsonrpc' }, mockFetch);
    assert.equal(alive, true);
  });

  it('报错或连接失败时返回 false', async () => {
    const mockFetch: typeof fetch = async () => {
      throw new Error('ECONNREFUSED');
    };
    const alive = await probeAria2Liveness({ rpcUrl: 'http://127.0.0.1:6800/jsonrpc' }, mockFetch);
    assert.equal(alive, false);
  });
});
