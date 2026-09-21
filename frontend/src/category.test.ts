import { describe, it, expect } from 'vitest';
import { normalizeExtensions, extractExtension, matchTaskCategory } from './lib/category';
import * as configModels from '../bindings/sheep-get/internal/config/models';

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

describe('matchTaskCategory', () => {
  const defaultBuiltins: configModels.CategoryConfig[] = [
    new configModels.CategoryConfig({
      id: 'builtin-video',
      name: '视频',
      directory: '/dl/Videos',
      extensions: ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'ts', 'm3u8'],
      isBuiltin: true,
    }),
    new configModels.CategoryConfig({
      id: 'builtin-audio',
      name: '音频',
      directory: '/dl/Audio',
      extensions: ['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'wma', 'opus'],
      isBuiltin: true,
    }),
    new configModels.CategoryConfig({
      id: 'builtin-image',
      name: '图片',
      directory: '/dl/Images',
      extensions: ['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico', 'tiff'],
      isBuiltin: true,
    }),
    new configModels.CategoryConfig({
      id: 'builtin-archive',
      name: '压缩包',
      directory: '/dl/Archives',
      extensions: ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'tgz'],
      isBuiltin: true,
    }),
    new configModels.CategoryConfig({
      id: 'builtin-software',
      name: '软件',
      directory: '/dl/Software',
      extensions: ['exe', 'msi', 'dmg', 'pkg', 'deb', 'rpm', 'apk', 'appimage', 'iso'],
      isBuiltin: true,
    }),
    new configModels.CategoryConfig({
      id: 'builtin-file',
      name: '文件',
      directory: '/dl/Files',
      extensions: ['doc', 'docx', 'pdf', 'txt', 'xls', 'xlsx', 'ppt', 'pptx', 'epub'],
      isBuiltin: true,
    }),
  ];

  const mockSettings = (
    customCategories: configModels.CategoryConfig[] = [],
    builtinCategories: configModels.CategoryConfig[] = defaultBuiltins,
  ): configModels.Settings => {
    return new configModels.Settings({
      download: new configModels.DownloadConfig({
        builtinCategories,
        customCategories,
      }),
    });
  };

  it('matches builtin categories by extension', () => {
    const settings = mockSettings();
    expect(matchTaskCategory('movie.mp4', settings)).toBe('builtin-video');
    expect(matchTaskCategory('song.mp3', settings)).toBe('builtin-audio');
    expect(matchTaskCategory('photo.png', settings)).toBe('builtin-image');
    expect(matchTaskCategory('data.tar.gz', settings)).toBe('builtin-archive');
    expect(matchTaskCategory('setup.exe', settings)).toBe('builtin-software');
    expect(matchTaskCategory('report.pdf', settings)).toBe('builtin-file');
  });

  it('falls back to builtin-file for unmatched or missing extension', () => {
    const settings = mockSettings();
    expect(matchTaskCategory('mystery.xyz123', settings)).toBe('builtin-file');
    expect(matchTaskCategory('Makefile', settings)).toBe('builtin-file');
    expect(matchTaskCategory('unknown', null)).toBe('builtin-file');
  });

  it('prioritizes custom categories over builtin categories', () => {
    const customWork = new configModels.CategoryConfig({
      id: 'custom-work',
      name: '工作压缩包',
      directory: '/work/archives',
      extensions: ['rar'],
      isBuiltin: false,
    });
    const settingsWithCustom = mockSettings([customWork]);
    expect(matchTaskCategory('package.rar', settingsWithCustom)).toBe('custom-work');
    // Other archives still match builtin-archive
    expect(matchTaskCategory('normal.zip', settingsWithCustom)).toBe('builtin-archive');

    // After removing custom category, falls back to builtin-archive
    const settingsWithoutCustom = mockSettings([]);
    expect(matchTaskCategory('package.rar', settingsWithoutCustom)).toBe('builtin-archive');
  });

  it('follows custom category order from top to bottom', () => {
    const catA = new configModels.CategoryConfig({
      id: 'cat-a',
      name: '分类A',
      extensions: ['log'],
    });
    const catB = new configModels.CategoryConfig({
      id: 'cat-b',
      name: '分类B',
      extensions: ['log'],
    });

    const settingsAB = mockSettings([catA, catB]);
    expect(matchTaskCategory('server.log', settingsAB)).toBe('cat-a');

    const settingsBA = mockSettings([catB, catA]);
    expect(matchTaskCategory('server.log', settingsBA)).toBe('cat-b');
  });
});
