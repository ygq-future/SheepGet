import { describe, it, expect } from 'vitest';
import { normalizeExtensions, extractExtension, resolveCategory } from './lib/category';
import * as configModels from '../bindings/sheep-get/internal/config/models';

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

  it('resolves builtin categories and fallbacks', () => {
    const settings = new configModels.Settings({
      download: new configModels.DownloadConfig({
        defaultDirectory: '/downloads',
        builtinCategories: [
          new configModels.CategoryConfig({
            id: 'builtin-video',
            name: '视频',
            directory: '/downloads/Videos',
            extensions: ['mp4', 'mkv'],
            isBuiltin: true,
          }),
          new configModels.CategoryConfig({
            id: 'builtin-file',
            name: '文件',
            directory: '/downloads/Files',
            extensions: ['pdf', 'doc'],
            isBuiltin: true,
          }),
        ],
        customCategories: [],
      }),
    });

    // Matches video
    const video = resolveCategory('clip.mp4', settings);
    expect(video.category?.name).toBe('视频');
    expect(video.directory).toBe('/downloads/Videos');

    // Matches document in file
    const doc = resolveCategory('readme.pdf', settings);
    expect(doc.category?.name).toBe('文件');
    expect(doc.directory).toBe('/downloads/Files');

    // Unmatched extension falls back to 文件
    const unknown = resolveCategory('data.unknown123', settings);
    expect(unknown.category?.name).toBe('文件');
    expect(unknown.directory).toBe('/downloads/Files');

    // No extension falls back to 文件
    const noExt = resolveCategory('Makefile', settings);
    expect(noExt.category?.name).toBe('文件');
    expect(noExt.directory).toBe('/downloads/Files');
  });

  it('prioritizes custom categories and respects ordering and overlapping rules', () => {
    const settings = new configModels.Settings({
      download: new configModels.DownloadConfig({
        defaultDirectory: '/downloads',
        builtinCategories: [
          new configModels.CategoryConfig({
            id: 'builtin-archive',
            name: '压缩包',
            directory: '/downloads/Archives',
            extensions: ['zip', 'rar', '7z'],
            isBuiltin: true,
          }),
          new configModels.CategoryConfig({
            id: 'builtin-file',
            name: '文件',
            directory: '/downloads/Files',
            extensions: ['doc'],
            isBuiltin: true,
          }),
        ],
        customCategories: [
          new configModels.CategoryConfig({
            id: 'custom-top',
            name: '置顶自定义',
            directory: '/custom/top',
            extensions: ['rar', 'log'],
            isBuiltin: false,
          }),
          new configModels.CategoryConfig({
            id: 'custom-bottom',
            name: '次级自定义',
            directory: '/custom/bottom',
            extensions: ['log'],
            isBuiltin: false,
          }),
        ],
      }),
    });

    // 1. rar is in custom-top and builtin-archive; custom-top must win!
    const rarRes = resolveCategory('test.rar', settings);
    expect(rarRes.category?.name).toBe('置顶自定义');
    expect(rarRes.directory).toBe('/custom/top');

    // 2. zip is only in builtin-archive; matches builtin-archive!
    const zipRes = resolveCategory('test.zip', settings);
    expect(zipRes.category?.name).toBe('压缩包');
    expect(zipRes.directory).toBe('/downloads/Archives');

    // 3. log is in both custom-top and custom-bottom; custom-top (first in order) must win!
    const logRes = resolveCategory('app.log', settings);
    expect(logRes.category?.name).toBe('置顶自定义');

    // 4. Remove custom category; rar must immediately revert to builtin-archive!
    settings.download.customCategories = [];
    const reverted = resolveCategory('test.rar', settings);
    expect(reverted.category?.name).toBe('压缩包');
    expect(reverted.directory).toBe('/downloads/Archives');
  });
});
