import { describe, it, expect } from 'vitest';
import { normalizeExtensions, extractExtension } from './lib/category';

// 分类规则的解析由后端负责（见 internal/config 的 ResolveCategory 测试）；
// 这里只覆盖仍在前端使用的输入归一化工具。
describe('category utilities', () => {
  it('normalizes extensions correctly', () => {
    expect(normalizeExtensions(['.MP4', 'mkv', ' .AVI ', 'mp4'])).toEqual(['mp4', 'mkv', 'avi']);
    expect(normalizeExtensions('rar, .7z, zip, RAR')).toEqual(['rar', '7z', 'zip']);
    expect(normalizeExtensions('')).toEqual([]);
  });

  it('extracts extension accurately', () => {
    expect(extractExtension('video.mp4')).toBe('mp4');
    expect(extractExtension('archive.tar.gz')).toBe('gz');
    expect(extractExtension('image.PNG?size=large#hash')).toBe('png');
    expect(extractExtension('no_ext')).toBe('');
    expect(extractExtension('trailing_dot.')).toBe('');
  });
});
