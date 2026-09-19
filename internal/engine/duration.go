package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"sheep-get/internal/credentials"
	"sheep-get/internal/mediainfo"
)

// 一次时长探测最多发几次范围请求。解析只需两段：文件头一段（faststart 的 mp4、
// mkv/webm 都在里面），未 faststart 的 mp4 再补文件尾一段。多出来的读一律拒绝——
// 时长是附加信息，不值得为它反复请求同一个文件。
const mediaProbeRangeBudget = 2

// ProbeMediaDuration 读远端媒体文件的时长（秒）。
//
// 它只读容器头部（必要时加尾部）那两段字节，不下载文件本体；文件名不是我们能读的容器时
// 一次请求都不发——时长是给界面看的附加信息，不该为它给每次下载都多发请求。
// 读不出来返回 false，由调用方决定怎么显示，绝不编一个数值出来。
func (d *HTTPDownloader) ProbeMediaDuration(ctx context.Context, urlStr, filename string, creds credentials.RequestCredentials, totalBytes int64) (float64, bool) {
	if !mediainfo.Supported(filename) {
		return 0, false
	}
	source := &remoteMediaSource{
		downloader: d,
		ctx:        ctx,
		url:        urlStr,
		creds:      creds,
		size:       totalBytes,
		budget:     mediaProbeRangeBudget,
	}
	return mediainfo.FromSource(filename, source)
}

// remoteMediaSource 是远端文件的随机读视图：解析器要哪一段就取哪一段，整段留在内存里给
// 解析用（解析本身在片段内完成，不会二次请求）。它只在一次解析里使用，单 goroutine，无需加锁。
type remoteMediaSource struct {
	downloader *HTTPDownloader
	ctx        context.Context
	url        string
	creds      credentials.RequestCredentials
	size       int64
	budget     int
}

func (s *remoteMediaSource) Size() int64 { return s.size }

func (s *remoteMediaSource) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	data, err := s.fetch(off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	n := copy(p, data)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// fetch 取回 [off, off+length) 这一段。
func (s *remoteMediaSource) fetch(off, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("empty range")
	}
	if s.budget <= 0 {
		return nil, errors.New("media probe range budget exhausted")
	}
	s.budget--

	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+length-1))
	req.Header.Set("User-Agent", UserAgentChrome)
	req.Header.Set("Accept", "*/*")
	applyRequestHeaders(req, s.creds)
	req.Close = true

	resp, err := s.downloader.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("media probe failed: server returned %s", resp.Status)
	}
	if resp.StatusCode == http.StatusOK && off > 0 {
		// 服务器忽略了范围请求：要拿尾部片段就得先把它前面的内容整段读掉，代价可能是一整个文件。
		// 这个时长不值这个钱，直接放弃。
		return nil, errors.New("server ignores range requests")
	}

	// 服务器也可能把整份文件给回来：只读我们要的长度，剩下的立刻丢掉。
	data, err := io.ReadAll(io.LimitReader(resp.Body, length))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, io.EOF
	}
	return data, nil
}
