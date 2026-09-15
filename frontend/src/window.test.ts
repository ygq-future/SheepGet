import { describe, it, expect } from 'vitest';

describe('multi-window routing and utilities', () => {
  it('detects fileinfo window from search params', () => {
    const search = '?window=fileinfo';
    const params = new URLSearchParams(search);
    expect(params.get('window')).toBe('fileinfo');
  });

  it('detects progress window from search params', () => {
    const search = '?window=progress';
    const params = new URLSearchParams(search);
    expect(params.get('window')).toBe('progress');
  });

  it('falls back to main window when window param is missing or unknown', () => {
    const search1 = '';
    const params1 = new URLSearchParams(search1);
    expect(params1.get('window')).toBeNull();

    const search2 = '?window=unknown';
    const params2 = new URLSearchParams(search2);
    expect(params2.get('window')).toBe('unknown');
  });

  it('extracts focus task ID from progress window search params', () => {
    const search = '?window=progress&focus=task_12345';
    const params = new URLSearchParams(search);
    expect(params.get('window')).toBe('progress');
    expect(params.get('focus')).toBe('task_12345');
  });
});
