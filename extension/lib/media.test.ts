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
