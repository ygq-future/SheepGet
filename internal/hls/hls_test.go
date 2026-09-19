package hls_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/hls"
)

func mustParse(t *testing.T, base, body string) *hls.ParseResult {
	t.Helper()
	parsed, err := hls.Parse(mustURL(t, base), []byte(body))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	return parsed
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("无效的基准地址: %v", err)
	}
	return parsed
}

const masterPlaylist = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="中文",LANGUAGE="zh",DEFAULT=YES,URI="audio/zh.m3u8",CHANNELS="2"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="English",LANGUAGE="en",DEFAULT=NO,URI="audio/en.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720,FRAME-RATE=30,CODECS="avc1.64001f,mp4a.40.2",AUDIO="aud"
v720/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,FRAME-RATE=60,AUDIO="aud"
https://cdn.example.com/v1080/index.m3u8
`

func TestParseMasterPlaylist(t *testing.T) {
	parsed := mustParse(t, "https://cdn.example.com/hls/master.m3u8", masterPlaylist)
	if parsed.Kind != hls.KindMaster {
		t.Fatalf("期望主清单，实际 %q", parsed.Kind)
	}

	variants := parsed.Master.Variants
	if len(variants) != 2 {
		t.Fatalf("期望 2 个清晰度，实际 %d", len(variants))
	}
	low := variants[0]
	if low.Width != 1280 || low.Height != 720 || low.Bandwidth != 2000000 || low.FrameRate != 30 {
		t.Fatalf("720p 属性不对: %+v", low)
	}
	// 引号里的逗号是 CODECS 的一部分，不是属性分隔符。
	if low.Codecs != "avc1.64001f,mp4a.40.2" {
		t.Fatalf("CODECS 被截断了: %q", low.Codecs)
	}
	if low.URI != "https://cdn.example.com/hls/v720/index.m3u8" {
		t.Fatalf("相对地址没有解析成绝对地址: %q", low.URI)
	}
	if low.AudioGroupID != "aud" {
		t.Fatalf("没有关联到音轨分组: %q", low.AudioGroupID)
	}
	if variants[1].URI != "https://cdn.example.com/v1080/index.m3u8" {
		t.Fatalf("绝对地址不该被改写: %q", variants[1].URI)
	}

	if len(parsed.Master.Media) != 2 {
		t.Fatalf("期望 2 条 EXT-X-MEDIA，实际 %d", len(parsed.Master.Media))
	}
	// 同一分组里优先取标记为默认的那一条。
	if got := hls.PickAudioURI(parsed.Master.Media, "aud"); got != "https://cdn.example.com/hls/audio/zh.m3u8" {
		t.Fatalf("默认音轨选择不对: %q", got)
	}
	if got := hls.PickAudioURI(parsed.Master.Media, "missing"); got != "" {
		t.Fatalf("分组不存在时应返回空，实际 %q", got)
	}
}

const mediaPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:5
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x00000000000000000000000000000005
#EXTINF:9.009,
seg0.ts
#EXTINF:9.009,
seg1.ts
#EXT-X-KEY:METHOD=NONE
#EXTINF:5.000,
seg2.ts
#EXT-X-ENDLIST
`

func TestParseMediaPlaylist(t *testing.T) {
	parsed := mustParse(t, "https://cdn.example.com/hls/v720/index.m3u8", mediaPlaylist)
	if parsed.Kind != hls.KindMedia {
		t.Fatalf("期望媒体清单，实际 %q", parsed.Kind)
	}

	pl := parsed.Media
	if pl.Version != 3 || pl.TargetDuration != 10 || pl.MediaSequence != 5 || !pl.EndList {
		t.Fatalf("清单头不对: %+v", pl)
	}
	if len(pl.Segments) != 3 {
		t.Fatalf("期望 3 个分片，实际 %d", len(pl.Segments))
	}
	if got := pl.Segments[0].URI; got != "https://cdn.example.com/hls/v720/seg0.ts" {
		t.Fatalf("分片地址没有解析成绝对地址: %q", got)
	}
	// 媒体序号是密钥推导的依据，必须逐个分片正确地累加。
	for i, want := range []int{5, 6, 7} {
		if pl.Segments[i].MediaSeq != want {
			t.Fatalf("第 %d 个分片的媒体序号应为 %d，实际 %d", i, want, pl.Segments[i].MediaSeq)
		}
	}
	if pl.Segments[0].Key == nil || pl.Segments[1].Key == nil {
		t.Fatal("加密标记没有落到分片上")
	}
	if got := pl.Segments[0].Key.URI; got != "https://cdn.example.com/hls/v720/key.bin" {
		t.Fatalf("密钥地址没有解析成绝对地址: %q", got)
	}
	if len(pl.Segments[0].Key.IV) != 16 || pl.Segments[0].Key.IV[15] != 0x05 {
		t.Fatalf("IV 解析不对: %x", pl.Segments[0].Key.IV)
	}
	if pl.Segments[2].Key != nil {
		t.Fatal("METHOD=NONE 之后的分片不应再带密钥")
	}
	if want := 23.018; pl.Duration < want-0.001 || pl.Duration > want+0.001 {
		t.Fatalf("总时长应为 %.3f，实际 %.3f", want, pl.Duration)
	}
}

func TestParseByteRangeAndInitSection(t *testing.T) {
	const body = `#EXTM3U
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.0,
#EXT-X-BYTERANGE:1000@0
all.m4s
#EXTINF:6.0,
#EXT-X-BYTERANGE:1200
all.m4s
#EXT-X-ENDLIST
`
	parsed := mustParse(t, "https://cdn.example.com/v/index.m3u8", body)
	pl := parsed.Media
	if len(pl.Segments) != 2 {
		t.Fatalf("期望 2 个分片，实际 %d", len(pl.Segments))
	}
	if pl.Segments[0].Map == nil || pl.Segments[0].Map.URI != "https://cdn.example.com/v/init.mp4" {
		t.Fatalf("初始化片段没有解析出来: %+v", pl.Segments[0].Map)
	}
	if pl.Segments[0].RangeStart != 0 || pl.Segments[0].RangeLength != 1000 {
		t.Fatalf("第一段范围不对: %+v", pl.Segments[0])
	}
	// 省略 @起始 时按上一段的结束位置接着算（RFC 8216 §4.3.2.2）。
	if pl.Segments[1].RangeStart != 1000 || pl.Segments[1].RangeLength != 1200 {
		t.Fatalf("第二段范围没有接着算: %+v", pl.Segments[1])
	}
	if got := pl.KnownSize(); got != 2200 {
		t.Fatalf("清单给全了长度时应能报出总大小，实际 %d", got)
	}
}

func TestParseRejectsNonPlaylist(t *testing.T) {
	for name, body := range map[string]string{
		"缺少 EXTM3U": "hello world\n",
		"空内容":       "",
		"只有头部":      "#EXTM3U\n#EXT-X-VERSION:3\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := hls.Parse(mustURL(t, "https://cdn.example.com/a.m3u8"), []byte(body)); err == nil {
				t.Fatal("期望报错，实际通过了")
			}
		})
	}
}

func TestMediaPlaylistWithoutSegmentsIsRejected(t *testing.T) {
	if _, err := hls.Parse(mustURL(t, "https://cdn.example.com/a.m3u8"),
		[]byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXT-X-ENDLIST\n")); err == nil {
		t.Fatal("没有分片的媒体清单不应被接受")
	}
}

func TestKnownSizeIsUnknownWithoutByteRange(t *testing.T) {
	parsed := mustParse(t, "https://cdn.example.com/a.m3u8", mediaPlaylist)
	if got := parsed.Media.KnownSize(); got != -1 {
		t.Fatalf("没有 BYTERANGE 时应报未知（-1），实际 %d", got)
	}
}

func TestSequenceIVFollowsMediaSequence(t *testing.T) {
	if got := hls.SequenceIV(0); !bytes.Equal(got, make([]byte, 16)) {
		t.Fatalf("序号 0 的 IV 应当全零，实际 %x", got)
	}
	got := hls.SequenceIV(0x1f)
	if len(got) != 16 || got[15] != 0x1f {
		t.Fatalf("序号应当作为 128 位大端整数左对齐，实际 %x", got)
	}
	for _, b := range got[:15] {
		if b != 0 {
			t.Fatalf("序号的高位不该被写脏: %x", got)
		}
	}
}

func TestDecryptAES128RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := hls.SequenceIV(7)
	plain := []byte("一份足够长的分片内容，用来跨过多个分组。")

	encrypted := encryptAES128(t, plain, key, iv)
	got, err := hls.DecryptAES128(encrypted, key, iv)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("解密结果不对: %q", got)
	}
}

// TestDecryptAES128KeepsExactLengthPayload 覆盖不带 PKCS7 填充的服务器：尾部字节不像填充时
// 必须原样保留，不能按「最后一个字节是几就裁几个」处理，那会把真实数据切掉。
func TestDecryptAES128KeepsExactLengthPayload(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := make([]byte, 16)
	plain := bytes.Repeat([]byte{0xAA}, 32)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("构造 cipher 失败: %v", err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, plain)

	got, err := hls.DecryptAES128(encrypted, key, iv)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("无填充的分片被切短了: 期望 %d 字节，实际 %d 字节", len(plain), len(got))
	}
}

func TestDecryptAES128RejectsBadInput(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := make([]byte, 16)
	if _, err := hls.DecryptAES128(make([]byte, 16), []byte("short"), iv); err == nil {
		t.Fatal("不足 16 字节的密钥应当报错")
	}
	if _, err := hls.DecryptAES128(make([]byte, 16), key, []byte("short")); err == nil {
		t.Fatal("不足 16 字节的 IV 应当报错")
	}
	if _, err := hls.DecryptAES128(make([]byte, 15), key, iv); err == nil {
		t.Fatal("长度不是 16 整数倍的密文应当报错")
	}
}

func encryptAES128(t *testing.T, plain, key, iv []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("构造 cipher 失败: %v", err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

// fakeOrigin 是一台可控的 HLS 源站，用来验证下载行为本身而不是网络。
type fakeOrigin struct {
	server *httptest.Server
	// hits 记录每个路径被请求的次数（含被服务器拒绝的那些）。
	hits map[string]int
	mu   sync.Mutex
	// failTimes 是「前 n 次请求返回 500」的路径；用它验证重试确实发生过。
	failTimes map[string]int
}

func newFakeOrigin(t *testing.T) *fakeOrigin {
	t.Helper()
	o := &fakeOrigin{hits: map[string]int{}, failTimes: map[string]int{}}
	o.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		o.hits[r.URL.Path]++
		count := o.hits[r.URL.Path]
		failFor := o.failTimes[r.URL.Path]
		o.mu.Unlock()

		if count <= failFor {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		o.serve(w, r)
	}))
	t.Cleanup(o.server.Close)
	return o
}

func (o *fakeOrigin) url(path string) string { return o.server.URL + path }

func (o *fakeOrigin) hitCount(path string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hits[path]
}

func (o *fakeOrigin) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, ".ts"):
		_, _ = fmt.Fprintf(w, "segment payload for %s\n", r.URL.Path)
	case r.URL.Path == "/key.bin":
		_, _ = w.Write([]byte("0123456789abcdef"))
	case r.URL.Path == "/init.mp4":
		_, _ = w.Write([]byte("init section bytes"))
	default:
		http.NotFound(w, r)
	}
}

func (o *fakeOrigin) mediaPlaylist(segments ...string) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-VERSION:3\n")
	for _, name := range segments {
		_, _ = fmt.Fprintf(&sb, "#EXTINF:6.000,\n%s\n", name)
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

func parseBody(t *testing.T, base, body string) *hls.MediaPlaylist {
	t.Helper()
	parsed := mustParse(t, base, body)
	if parsed.Kind != hls.KindMedia {
		t.Fatalf("期望媒体清单，实际 %q", parsed.Kind)
	}
	return parsed.Media
}

func newTestFetcher() *hls.Fetcher {
	return hls.NewFetcher(nil, credentials.New(map[string]string{"Referer": "https://site.example/watch"}))
}

func TestDownloadFetchesEverySegment(t *testing.T) {
	origin := newFakeOrigin(t)
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts", "seg1.ts", "seg2.ts"))

	dir := t.TempDir()
	inputs, err := newTestFetcher().Download(context.Background(), pl, dir, 4, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if len(inputs.Segments) != 3 || len(inputs.Init) != 0 {
		t.Fatalf("输入不对: %+v", inputs)
	}
	// 落点要记住的是文件名而不是绝对路径：它要随任务一起持久化。
	for _, name := range inputs.Segments {
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("分片落点应当是文件名，实际 %q", name)
		}
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("分片没有落盘: %v", statErr)
		}
	}
}

func TestDownloadResumesAndRetries(t *testing.T) {
	origin := newFakeOrigin(t)
	origin.failTimes["/seg1.ts"] = 1
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts", "seg1.ts", "seg2.ts"))

	dir := t.TempDir()
	// 预先放一个「上一轮已经下完」的分片：续传必须跳过它，不再发请求。
	if err := os.WriteFile(filepath.Join(dir, "seg_00000.ts"), []byte("already complete"), 0o644); err != nil {
		t.Fatalf("无法预置分片: %v", err)
	}

	inputs, err := newTestFetcher().Download(context.Background(), pl, dir, 4, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if got := origin.hitCount("/seg0.ts"); got != 0 {
		t.Fatalf("已完整落盘的分片不该再取一次，实际请求了 %d 次", got)
	}
	if got := origin.hitCount("/seg1.ts"); got != 2 {
		t.Fatalf("失败一次后应当重试，实际请求了 %d 次", got)
	}
	// 预置的分片内容不该被覆盖。
	if content, readErr := os.ReadFile(filepath.Join(dir, inputs.Segments[0])); readErr != nil || string(content) != "already complete" {
		t.Fatalf("续传时改写了已有分片: %q %v", content, readErr)
	}
}

func TestDownloadReportsProgress(t *testing.T) {
	origin := newFakeOrigin(t)
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts", "seg1.ts"))

	// 分片是并发下载的，进度回调会被多个 goroutine 同时调用。
	var mu sync.Mutex
	var lastDownloaded int64
	var lastCompleted, lastTotal int
	var lastDone []bool
	_, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 2,
		func(downloaded, _ int64, completed, totalCount int, done []bool) {
			mu.Lock()
			defer mu.Unlock()
			lastDownloaded, lastCompleted, lastTotal = downloaded, completed, totalCount
			lastDone = done
		})
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if lastCompleted != 2 || lastTotal != 2 {
		t.Fatalf("进度条数不对: %d/%d", lastCompleted, lastTotal)
	}
	if lastDownloaded <= 0 {
		t.Fatalf("进度字节数不对: %d", lastDownloaded)
	}
	if len(lastDone) != 2 || !lastDone[0] || !lastDone[1] {
		t.Fatalf("分片完成位图不对: %v", lastDone)
	}
}

func TestDownloadFailsAfterExhaustingRetries(t *testing.T) {
	origin := newFakeOrigin(t)
	origin.failTimes["/seg1.ts"] = 99
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts", "seg1.ts"))

	_, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 2, nil)
	if err == nil {
		t.Fatal("一直失败的分片应当让下载失败")
	}
	if got := origin.hitCount("/seg1.ts"); got != 3 {
		t.Fatalf("应当尝试 3 次，实际 %d 次", got)
	}
}

func TestDownloadStopsWhenCanceled(t *testing.T) {
	origin := newFakeOrigin(t)
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestFetcher().Download(ctx, pl, t.TempDir(), 1, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled，实际 %v", err)
	}
}

func TestDownloadDecryptsAES128(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("第一段明文的实际内容，长度跨过多个分组。")
	// 带显式 IV 的分片用清单给的向量，不带 IV 的按媒体序号推导。
	withIV := encryptAES128(t, plain, key, hls.SequenceIV(3))
	derived := encryptAES128(t, plain, key, hls.SequenceIV(4))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/key.bin":
			_, _ = w.Write(key)
		case "/enc0.ts":
			_, _ = w.Write(withIV)
		case "/enc1.ts":
			_, _ = w.Write(derived)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:3
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x00000000000000000000000000000003
#EXTINF:6.000,
enc0.ts
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
#EXTINF:6.000,
enc1.ts
#EXT-X-ENDLIST
`
	pl := parseBody(t, server.URL+"/media.m3u8", body)
	dir := t.TempDir()
	inputs, err := newTestFetcher().Download(context.Background(), pl, dir, 1, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}

	for _, name := range inputs.Segments {
		content, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("分片没有落盘: %v", readErr)
		}
		if !bytes.Equal(content, plain) {
			t.Fatalf("%s 解密结果不对: %q", name, content)
		}
	}
}

func TestDownloadRejectsUnsupportedEncryption(t *testing.T) {
	origin := newFakeOrigin(t)
	body := `#EXTM3U
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="key.bin"
#EXTINF:6.000,
seg0.ts
#EXT-X-ENDLIST
`
	pl := parseBody(t, origin.url("/media.m3u8"), body)
	_, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "AES-128") {
		t.Fatalf("不该支持的加密方法要明确报出来，实际 %v", err)
	}
}

func TestDownloadFetchesInitSection(t *testing.T) {
	origin := newFakeOrigin(t)
	body := `#EXTM3U
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.000,
seg0.ts
#EXT-X-ENDLIST
`
	pl := parseBody(t, origin.url("/media.m3u8"), body)
	inputs, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 1, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if len(inputs.Init) != 1 || inputs.Init[0] != "init.mp4" {
		t.Fatalf("初始化片段没有记进输入: %+v", inputs.Init)
	}
	if got := origin.hitCount("/init.mp4"); got != 1 {
		t.Fatalf("初始化片段应当只取一次，实际 %d 次", got)
	}
}

func TestDownloadRejectsChangingInitSection(t *testing.T) {
	origin := newFakeOrigin(t)
	body := `#EXTM3U
#EXT-X-MAP:URI="init-a.mp4"
#EXTINF:6.000,
seg0.ts
#EXT-X-MAP:URI="init-b.mp4"
#EXTINF:6.000,
seg1.ts
#EXT-X-ENDLIST
`
	pl := parseBody(t, origin.url("/media.m3u8"), body)
	_, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "初始化片段") {
		t.Fatalf("中途换初始化片段应明确报不支持，实际 %v", err)
	}
}

func TestDownloadCarriesRequestContext(t *testing.T) {
	var mu sync.Mutex
	var seenReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenReferer = r.Header.Get("Referer")
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ".ts") {
			_, _ = w.Write([]byte("payload"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	pl := parseBody(t, server.URL+"/media.m3u8", "#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	if _, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 1, nil); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seenReferer != "https://site.example/watch" {
		t.Fatalf("请求上下文没有带上: %q", seenReferer)
	}
}

func TestProbeSizeSumsSegmentLengths(t *testing.T) {
	origin := newFakeOrigin(t)
	pl := parseBody(t, origin.url("/media.m3u8"), origin.mediaPlaylist("seg0.ts", "seg1.ts", "seg2.ts"))

	if total := newTestFetcher().ProbeSize(context.Background(), []*hls.MediaPlaylist{pl}, 0); total <= 0 {
		t.Fatalf("应当问出总大小，实际 %d", total)
	}

	// 服务器给不出长度时老实报未知，不做按码率推算。
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer broken.Close()
	missing := parseBody(t, broken.URL+"/media.m3u8", "#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	if got := newTestFetcher().ProbeSize(context.Background(), []*hls.MediaPlaylist{missing}, 0); got != -1 {
		t.Fatalf("长度拿不全时应报未知（-1），实际 %d", got)
	}
}

func TestProbeSizeGivesUpAboveSegmentBudget(t *testing.T) {
	pl := &hls.MediaPlaylist{Segments: []hls.Segment{{URI: "a.ts"}, {URI: "b.ts"}, {URI: "c.ts"}}}
	if got := newTestFetcher().ProbeSize(context.Background(), []*hls.MediaPlaylist{pl}, 2); got != -1 {
		t.Fatalf("超过预算应当不再询问，实际 %d", got)
	}
}

// TestDownloadAbortsWhenServerStallsMidBody 锁住「服务器发出半截响应后停住」的形态：
// 没有单次请求超时时，io.Copy 会永远等下去，8 个分片 worker 一起挂在半截响应上，
// 任务既不前进也不报错。超时到点后下载应当以错误收场，而不是挂死。
func TestDownloadAbortsWhenServerStallsMidBody(t *testing.T) {
	restore := hls.SetRequestTimeoutForTest(150 * time.Millisecond)
	defer restore()

	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-block // 响应发出一半后停住，永不结束
	}))
	defer func() {
		close(block) // 测试结束后放行 handler，让服务器能退出
		server.Close()
	}()

	pl := parseBody(t, server.URL+"/media.m3u8", "#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := newTestFetcher().Download(ctx, pl, t.TempDir(), 1, nil); err == nil {
		t.Fatalf("停住的服务器应当让下载以失败收场，而不是一直挂住")
	}
}

// TestDownloadFailsFastWhenByteStreamStalls 锁住「慢滴流后彻底停住」的形态：
// 服务器先以小间隔逐字节发送（合法的慢速传输，不该被总超时误杀），随后字节完全停止。
// 空闲看门狗应当在远小于总超时的时间内把下载斩断，并给出 ErrStalled 而不是笼统超时。
func TestDownloadFailsFastWhenByteStreamStalls(t *testing.T) {
	restore := hls.SetRequestTimeoutForTest(5 * time.Second)
	defer restore()
	oldIdle := hls.IdleTimeout()
	hls.SetIdleTimeoutForTest(80 * time.Millisecond)
	defer func() { hls.SetIdleTimeoutForTest(oldIdle) }()

	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		flusher := w.(http.Flusher)
		for i := 0; i < 10; i++ {
			_, _ = w.Write([]byte("x"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		<-block // 字节流停住，永不结束
	}))
	defer func() {
		close(block)
		server.Close()
	}()

	pl := parseBody(t, server.URL+"/media.m3u8", "#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n")
	start := time.Now()
	_, err := newTestFetcher().Download(context.Background(), pl, t.TempDir(), 1, nil)
	if err == nil {
		t.Fatalf("字节流停住的下载应当以失败收场")
	}
	if !errors.Is(err, hls.ErrStalled) {
		t.Fatalf("应当报停摆错误，实际 %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("空闲斩断应当远快于总超时，实际耗时 %v", elapsed)
	}
}

func TestInspectAndResolveMasterWithSeparateAudio(t *testing.T) {
	const servedMaster = `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="中文",DEFAULT=YES,URI="audio/zh.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720,AUDIO="aud"
v720/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,AUDIO="aud"
v1080/index.m3u8
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte(servedMaster))
		case "/v720/index.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n"))
		case "/audio/zh.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:12.500,\naudio0.ts\n#EXT-X-ENDLIST\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	f := newTestFetcher()
	masterURL := server.URL + "/master.m3u8"

	inspected, err := hls.Inspect(context.Background(), f, masterURL)
	if err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}
	if inspected.Kind != hls.KindMaster || len(inspected.Variants) != 2 {
		t.Fatalf("清单探测结果不对: %+v", inspected)
	}

	picked := inspected.Variants[0]
	sel, err := hls.Resolve(context.Background(), f, masterURL, picked,
		hls.PickAudioURI(inspected.Media, picked.AudioGroupID))
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if sel.Source.AudioURI != server.URL+"/audio/zh.m3u8" {
		t.Fatalf("独立音轨没有对上: %q", sel.Source.AudioURI)
	}
	if sel.Source.Segments != 2 {
		t.Fatalf("分片数应当把音轨算进去，实际 %d", sel.Source.Segments)
	}
	// 音轨更长时成品时长由它决定。
	if sel.Source.Duration != 12.5 {
		t.Fatalf("时长应当取较长的一条轨，实际 %v", sel.Source.Duration)
	}
	if sel.Source.TotalBytes != -1 {
		t.Fatalf("清单没给出分片长度时大小应当是未知，实际 %d", sel.Source.TotalBytes)
	}
	if sel.VideoPlaylist == nil || sel.AudioPlaylist == nil {
		t.Fatal("两条分片清单都要带回来")
	}
}

func TestInspectMediaPlaylistIsItsOwnOnlyVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.000,\nseg0.ts\n#EXT-X-ENDLIST\n"))
	}))
	defer server.Close()

	inspected, err := hls.Inspect(context.Background(), newTestFetcher(), server.URL+"/media.m3u8")
	if err != nil {
		t.Fatalf("Inspect 失败: %v", err)
	}
	if inspected.Kind != hls.KindMedia {
		t.Fatalf("期望媒体清单，实际 %q", inspected.Kind)
	}
	if len(inspected.Variants) != 1 || inspected.Variants[0].URI != server.URL+"/media.m3u8" {
		t.Fatalf("直接给出媒体清单时它自己就是唯一版本: %+v", inspected.Variants)
	}
}

func TestInputsPaths(t *testing.T) {
	inputs := &hls.Inputs{Init: []string{"init.mp4"}, Segments: []string{"seg_00000.ts"}}
	initPaths, segmentPaths := inputs.Paths("/tmp/segments")
	if len(initPaths) != 1 || initPaths[0] != filepath.Join("/tmp/segments", "init.mp4") {
		t.Fatalf("初始化片段路径不对: %v", initPaths)
	}
	if len(segmentPaths) != 1 || segmentPaths[0] != filepath.Join("/tmp/segments", "seg_00000.ts") {
		t.Fatalf("分片路径不对: %v", segmentPaths)
	}

	var empty *hls.Inputs
	if a, b := empty.Paths("/tmp"); a != nil || b != nil {
		t.Fatal("空输入应当展开成空路径")
	}
}
