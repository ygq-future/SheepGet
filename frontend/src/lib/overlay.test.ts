// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest';
import { acquireOverlayLock, resetOverlayLockForTesting } from './overlay';

describe('overlay lock manager', () => {
  beforeEach(() => {
    resetOverlayLockForTesting();
  });

  it('sets data-has-overlay attribute when first lock is acquired', () => {
    expect(document.documentElement.getAttribute('data-has-overlay')).toBeNull();

    const release = acquireOverlayLock();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBe('true');

    release();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBeNull();
  });

  it('handles nested locks gracefully and releases attribute only on last release', () => {
    const release1 = acquireOverlayLock();
    const release2 = acquireOverlayLock();

    expect(document.documentElement.getAttribute('data-has-overlay')).toBe('true');

    release1();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBe('true');

    release2();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBeNull();
  });

  it('ignores duplicate calls to the same release function', () => {
    const release = acquireOverlayLock();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBe('true');

    release();
    expect(document.documentElement.getAttribute('data-has-overlay')).toBeNull();

    release(); // idempotent
    expect(document.documentElement.getAttribute('data-has-overlay')).toBeNull();
  });
});
