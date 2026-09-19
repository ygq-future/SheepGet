import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { formatBytes, isMediaResponse, parseContentDispositionFilename } from './media';

describe('media detection', () => {
  it('detects HLS playlists by extension and Content-Type', () => {
    // 1. By extension .m3u8
    const res1 = isMediaResponse('https://example.com/live/playlist.m3u8');
    assert.equal(res1.isMedia, true);
    assert.equal(res1.isHls, true);

    // 2. By Content-Type
    const res2 = isMediaResponse(
      'https://example.com/stream?id=123',
      'application/vnd.apple.mpegurl; charset=utf-8',
    );
    assert.equal(res2.isMedia, true);
    assert.equal(res2.isHls, true);
  });

  it('detects common media streams by extension and MIME', () => {
    // MP4 by MIME
    const res1 = isMediaResponse('https://example.com/video', 'video/mp4');
    assert.equal(res1.isMedia, true);
    assert.equal(res1.isHls, false);
    assert.equal(res1.mime, 'video/mp4');

    // MKV by extension
    const res2 = isMediaResponse('https://example.com/files/movie.mkv');
    assert.equal(res2.isMedia, true);
    assert.equal(res2.isHls, false);

    // HTML / plain text is not media
    const res3 = isMediaResponse('https://example.com/index.html', 'text/html; charset=utf-8');
    assert.equal(res3.isMedia, false);
  });

  it('后缀判定必须被 Content-Type 否认：脚本与页面不是媒体', () => {
    // 开发服务器把 TypeScript 源码以 application/javascript 提供，路径后缀却是 .ts——
    // 与 MPEG-TS 分片同名，只看后缀必然把整个 src 目录列成可下载资源。
    assert.equal(
      isMediaResponse('https://app.example.com/src/main.ts', 'application/javascript').isMedia,
      false,
    );
    assert.equal(
      isMediaResponse(
        'https://app.example.com/src/router.ts',
        'application/javascript; charset=utf-8',
      ).isMedia,
      false,
    );
    assert.equal(
      isMediaResponse('https://app.example.com/src/config.ts', 'text/javascript').isMedia,
      false,
    );
    // 同一类：签名过期时 CDN 用 .mp4 的 URL 返回一个 HTML 错误页
    assert.equal(
      isMediaResponse('https://cdn.example.com/signed.mp4', 'text/html; charset=utf-8').isMedia,
      false,
    );
    assert.equal(
      isMediaResponse('https://cdn.example.com/data.json', 'application/json').isMedia,
      false,
    );
  });

  it('.ts 只在 Content-Type 佐证是媒体时才算媒体', () => {
    // 真实分片：响应头说明它就是 MPEG-TS
    const seg = isMediaResponse('https://cdn.example.com/hls/seg001.ts', 'video/mp2t');
    assert.equal(seg.isMedia, true);
    assert.equal(seg.isHls, false);

    // 没有信息时不猜：octet-stream 与缺失 Content-Type 都不足以佐证
    assert.equal(
      isMediaResponse('https://cdn.example.com/hls/seg002.ts', 'application/octet-stream').isMedia,
      false,
    );
    assert.equal(isMediaResponse('https://cdn.example.com/hls/seg003.ts').isMedia, false);
    assert.equal(isMediaResponse('https://cdn.example.com/hls/seg004.ts', '').isMedia, false);
  });

  it('HLS 列表不受否决规则影响：text/plain 提供的 m3u8 仍按后缀识别', () => {
    const res = isMediaResponse('https://cdn.example.com/live/playlist.m3u8', 'text/plain');
    assert.equal(res.isMedia, true);
    assert.equal(res.isHls, true);
  });

  it('parses Content-Disposition filenames', () => {
    assert.equal(parseContentDispositionFilename('attachment; filename="video.mp4"'), 'video.mp4');
    assert.equal(
      parseContentDispositionFilename("attachment; filename*=UTF-8''my%20clip.mp4"),
      'my clip.mp4',
    );
    assert.equal(parseContentDispositionFilename(''), '');
  });

  it('formats byte sizes cleanly', () => {
    assert.equal(formatBytes(0), '未知大小');
    assert.equal(formatBytes(-1), '未知大小');
    assert.equal(formatBytes(undefined), '未知大小');
    assert.equal(formatBytes(1024), '1.0 KB');
    assert.equal(formatBytes(104857600), '100.0 MB');
    assert.equal(formatBytes(10737418240), '10.0 GB');
  });
});
