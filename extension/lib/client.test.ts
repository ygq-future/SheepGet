import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { DesktopClient, discoverSessionViaHttp } from './client';
import { HeaderNames, Paths, Ports, WsEvents } from './protocol.generated';
import type { HandoverRequest } from './types';

describe('discoverSessionViaHttp', () => {
  it('discovers session when endpoint returns valid credentials', async () => {
    const mockFetch = async () =>
      new Response(
        JSON.stringify({
          status: 'ok',
          version: '1.0.0',
          port: 9248,
          sessionToken: 'test_token_1234567890',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );

    const session = await discoverSessionViaHttp(
      Ports.DefaultServer,
      mockFetch as unknown as typeof fetch,
    );
    assert.ok(session !== null);
    assert.equal(session?.port, 9248);
    assert.equal(session?.sessionToken, 'test_token_1234567890');
  });

  it('returns null when server is offline or errors', async () => {
    const mockFetch = async () => {
      throw new Error('Connection refused');
    };

    const session = await discoverSessionViaHttp(
      Ports.DefaultServer,
      mockFetch as unknown as typeof fetch,
    );
    assert.equal(session, null);
  });

  it('returns null when response status is not ok', async () => {
    const mockFetch = async () => new Response('Forbidden', { status: 403 });

    const session = await discoverSessionViaHttp(
      Ports.DefaultServer,
      mockFetch as unknown as typeof fetch,
    );
    assert.equal(session, null);
  });
});
describe('DesktopClient events', () => {
  it('dispatches onServerMigrated when server_migrated event arrives', () => {
    const client = new DesktopClient({ port: 9248, sessionToken: 'token_123' });
    let migratedPort = 0;

    // Simulate WebSocket environment
    const originalWebSocket = globalThis.WebSocket;
    let latestWs: any = null;

    class MockWebSocket {
      readyState = 1;
      onopen: (() => void) | null = null;
      onclose: (() => void) | null = null;
      onmessage: ((event: { data: string }) => void) | null = null;
      constructor() {
        latestWs = this;
      }
      close() {}
    }
    // @ts-expect-error test mock
    globalThis.WebSocket = MockWebSocket;

    try {
      const link = client.connectEvents(
        () => {},
        () => {},
        (newPort) => {
          migratedPort = newPort;
        },
      );

      latestWs?.onopen?.();
      latestWs?.onmessage?.({
        data: JSON.stringify({
          event: WsEvents.ServerMigrated,
          data: { port: 9250 },
        }),
      });

      assert.equal(migratedPort, 9250);
      link.close();
    } finally {
      globalThis.WebSocket = originalWebSocket;
    }
  });
});

describe('loopback request paths', () => {
  it('targets the protocol paths on the session port', async () => {
    const client = new DesktopClient({ port: Ports.DefaultServer, sessionToken: 'token_123' });
    const calls: Array<{ url: string; headers: Record<string, string> }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        url: String(input),
        headers: (init?.headers ?? {}) as Record<string, string>,
      });
      return new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as typeof fetch;
    try {
      await discoverSessionViaHttp(Ports.DefaultServer, globalThis.fetch);
      await client.ping();
      await client.sendHandover({ url: 'https://example.com/file.zip' } as HandoverRequest);
      await client.fetchHLSVariants('https://example.com/master.m3u8', {});
      await client.fetchMediaProbe({ url: 'https://example.com/video.mp4' }, {});
      await client.fetchTakeoverConfig();
    } finally {
      globalThis.fetch = originalFetch;
    }

    // 路径本身带协议前缀，端口只出现一次：两侧拼接重复前缀会让扩展对不上任何一个端点。
    const origin = `http://127.0.0.1:${Ports.DefaultServer}`;
    assert.deepEqual(
      calls.map((call) => call.url),
      [
        `${origin}${Paths.Discover}`,
        `${origin}${Paths.Ping}`,
        `${origin}${Paths.Handover}`,
        `${origin}${Paths.HLSVariants}`,
        `${origin}${Paths.MediaProbe}`,
        `${origin}${Paths.TakeoverConfig}`,
      ],
    );
    for (const call of calls.slice(1)) {
      assert.equal(call.headers[HeaderNames.Token], 'token_123');
    }
  });
});
