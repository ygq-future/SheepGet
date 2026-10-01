import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { ResponseFilenameCache, cleanFilename } from './filenames';

describe('ResponseFilenameCache', () => {
  it('按 URL 记住响应头里的真实文件名', () => {
    const cache = new ResponseFilenameCache();
    cache.remember('https://example.com/a', 'PowerShell-7.6.6-win-x64.msi');
    assert.equal(cache.lookup('https://example.com/a'), 'PowerShell-7.6.6-win-x64.msi');
  });

  it('finalUrl 与原始 url 都查不到时返回 undefined，调用方回落到下载项自带的名字', () => {
    const cache = new ResponseFilenameCache();
    cache.remember('https://example.com/a', 'file.msi');
    assert.equal(cache.lookup('https://example.com/b', 'https://example.com/c'), undefined);
    assert.equal(cache.lookup(undefined, undefined), undefined);
  });

  it('依次尝试多个 URL，命中第一个有记录的就返回', () => {
    const cache = new ResponseFilenameCache();
    cache.remember('https://example.com/original', 'file.msi');
    assert.equal(
      cache.lookup('https://example.com/final', 'https://example.com/original'),
      'file.msi',
    );
  });

  it('空 URL 或空文件名不写入，避免把无效值当成真名', () => {
    const cache = new ResponseFilenameCache();
    cache.remember('', 'file.msi');
    cache.remember('https://example.com/a', '');
    assert.equal(cache.lookup('', 'https://example.com/a'), undefined);
  });

  it('超过条数上限时淘汰最早的条目，不会无上限增长', () => {
    const cache = new ResponseFilenameCache(2);
    cache.remember('https://example.com/1', 'a.msi');
    cache.remember('https://example.com/2', 'b.msi');
    cache.remember('https://example.com/3', 'c.msi');
    assert.equal(cache.lookup('https://example.com/1'), undefined);
    assert.equal(cache.lookup('https://example.com/2'), 'b.msi');
    assert.equal(cache.lookup('https://example.com/3'), 'c.msi');
  });

  it('同一个 URL 重新记录按最近一次写入算，不会因为重复记录被提前淘汰', () => {
    const cache = new ResponseFilenameCache(2);
    cache.remember('https://example.com/1', 'a.msi');
    cache.remember('https://example.com/2', 'b.msi');
    cache.remember('https://example.com/1', 'a2.msi');
    cache.remember('https://example.com/3', 'c.msi');
    assert.equal(cache.lookup('https://example.com/2'), undefined);
    assert.equal(cache.lookup('https://example.com/1'), 'a2.msi');
    assert.equal(cache.lookup('https://example.com/3'), 'c.msi');
  });
});

describe('cleanFilename', () => {
  it('剥离 Windows 反斜杠绝对路径与目录前缀', () => {
    assert.equal(
      cleanFilename('E:\\library\\downloads\\google\\document_123.mp4'),
      'document_123.mp4',
    );
    assert.equal(cleanFilename('C:\\Downloads\\file.zip'), 'file.zip');
  });

  it('剥离 POSIX 正斜杠绝对路径与目录前缀', () => {
    assert.equal(cleanFilename('/home/user/downloads/archive.tar.gz'), 'archive.tar.gz');
    assert.equal(cleanFilename('folder/sub/media.mp4'), 'media.mp4');
  });

  it('去除包裹引号与首尾空白', () => {
    assert.equal(cleanFilename('  "video.mp4"  '), 'video.mp4');
    assert.equal(cleanFilename(" 'D:\\data\\music.flac' "), 'music.flac');
  });

  it('处理空串与异常输入', () => {
    assert.equal(cleanFilename(''), '');
    assert.equal(cleanFilename(undefined), '');
    assert.equal(cleanFilename('   '), '');
  });
});
