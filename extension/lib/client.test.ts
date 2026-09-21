import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_LOOPBACK_PORT, discoverSessionViaHttp } from './client';

describe('discoverSessionViaHttp', () => {
  it('has default loopback port 9248', () => {
    assert.equal(DEFAULT_LOOPBACK_PORT, 9248);
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
