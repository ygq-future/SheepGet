import { describe, it, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { isExtensionContextValid, safeSendMessage } from './runtime';

describe('runtime context and safe messaging', () => {
  const originalChrome = (globalThis as unknown as { chrome?: unknown }).chrome;

  afterEach(() => {
    (globalThis as unknown as { chrome?: unknown }).chrome = originalChrome;
  });

  it('returns false when chrome is undefined', () => {
    (globalThis as unknown as { chrome?: unknown }).chrome = undefined;
    assert.equal(isExtensionContextValid(), false);
  });

  it('returns false when chrome.runtime.id is missing or empty', () => {
    (globalThis as unknown as { chrome?: unknown }).chrome = { runtime: {} };
    assert.equal(isExtensionContextValid(), false);

    (globalThis as unknown as { chrome?: unknown }).chrome = { runtime: { id: '' } };
    assert.equal(isExtensionContextValid(), false);
  });

  it('returns true when chrome.runtime.id is a valid non-empty string', () => {
    (globalThis as unknown as { chrome?: unknown }).chrome = { runtime: { id: 'mock-ext-id' } };
    assert.equal(isExtensionContextValid(), true);
  });

  it('safeSendMessage returns false and triggers callback when context is invalid', () => {
    (globalThis as unknown as { chrome?: unknown }).chrome = undefined;
    let invalidated = false;
    const sent = safeSendMessage('test', undefined, {
      onContextInvalidated: () => {
        invalidated = true;
      },
    });

    assert.equal(sent, false);
    assert.equal(invalidated, true);
  });

  it('safeSendMessage catches synchronous Extension context invalidated error', () => {
    (globalThis as unknown as { chrome?: unknown }).chrome = {
      runtime: {
        id: 'mock-ext-id',
        sendMessage: () => {
          throw new Error('Extension context invalidated.');
        },
      },
    };

    let invalidated = false;
    const sent = safeSendMessage('test', undefined, {
      onContextInvalidated: () => {
        invalidated = true;
      },
    });

    assert.equal(sent, false);
    assert.equal(invalidated, true);
  });

  it('safeSendMessage detects asynchronous context invalidated via lastError', () => {
    const mockRuntime = {
      id: 'mock-ext-id',
      lastError: undefined as { message?: string } | undefined,
      sendMessage: (_msg: unknown, callback: (res?: unknown) => void) => {
        mockRuntime.lastError = { message: 'Extension context invalidated.' };
        callback(undefined);
      },
    };
    (globalThis as unknown as { chrome?: unknown }).chrome = { runtime: mockRuntime };

    let invalidated = false;
    let responseReceived = false;
    const sent = safeSendMessage(
      'test',
      () => {
        responseReceived = true;
      },
      {
        onContextInvalidated: () => {
          invalidated = true;
        },
      },
    );

    assert.equal(sent, true);
    assert.equal(invalidated, true);
    assert.equal(responseReceived, false);
  });

  it('safeSendMessage delivers response when communication succeeds', () => {
    const mockRuntime = {
      id: 'mock-ext-id',
      lastError: undefined,
      sendMessage: (_msg: unknown, callback: (res?: unknown) => void) => {
        callback({ success: true });
      },
    };
    (globalThis as unknown as { chrome?: unknown }).chrome = { runtime: mockRuntime };

    let response: unknown = null;
    const sent = safeSendMessage('test', (res) => {
      response = res;
    });

    assert.equal(sent, true);
    assert.deepEqual(response, { success: true });
  });
});
