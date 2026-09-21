import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_LOOPBACK_PORT,
  PORT_FALLBACK_SPAN,
  DesktopClient,
  discoverSessionViaHttp,
} from './client';

describe('discoverSessionViaHttp', () => {
  it('has default loopback port 9248', () => {
    assert.equal(DEFAULT_LOOPBACK_PORT, 9248);
  });
  it('has fallback span of 5 ports', () => {
    assert.equal(PORT_FALLBACK_SPAN, 5);
  });

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

    const session = await discoverSessionViaHttp(9248, mockFetch as unknown as typeof fetch);
    assert.ok(session !== null);
    assert.equal(session?.port, 9248);
    assert.equal(session?.sessionToken, 'test_token_1234567890');
  });

  it('returns null when server is offline or errors', async () => {
    const mockFetch = async () => {
      throw new Error('Connection refused');
    };

    const session = await discoverSessionViaHttp(9248, mockFetch as unknown as typeof fetch);
    assert.equal(session, null);
  });

  it('returns null when response status is not ok', async () => {
    const mockFetch = async () => new Response('Forbidden', { status: 403 });

    const session = await discoverSessionViaHttp(9248, mockFetch as unknown as typeof fetch);
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
          event: 'server_migrated',
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
