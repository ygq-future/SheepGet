package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sheep-get/internal/atomicfile"
	"sheep-get/internal/credentials"
)

const (
	// segmentFetchAttempts 是单个分片的下载尝试次数（含首次）。分片是独立小文件，
	// 一次失败重来即可，不必像大文件那样长退避。
	segmentFetchAttempts = 3
	// defaultSegmentConcurrency 是分片下载的默认并发数；调用方给的值更小时以调用方为准。
	defaultSegmentConcurrency = 8
)

// requestTimeout 是单次 HLS 请求（清单、密钥、分片、大小探测）愿意等一个响应的上限。
// 下载器的 HTTP 客户端整体超时是 0——大文件的传输不该有总时长限制；但 HLS 的每个请求都
// 小而独立，服务器在响应中途停住时，没有这个上限一次卡住就会挂住整个任务：8 个分片
// worker 全部停在半截响应上，进度既不前进也不报错。到点按失败处理，fetchSegment 的
// 既有重试会再试，最终把错误报给界面，而不是让任务永远停在 0%。
// 用 var 而非 const 是为了测试能把等它超时的用例跑到毫秒级。
var requestTimeout = 45 * time.Second

// idleTimeout 是「连续这么久没有读到任何字节」的判定上限。总超时兜住整个响应的时长，
// 但大分片在慢速服务器上合法地传输很久，一刀切的总超时会误杀慢而不死的下载；
// 停摆连接的特征是字节完全停止，15 秒静默就能判定，不必等总超时。
var idleTimeout = 15 * time.Second

// ErrStalled 表示响应在读取途中停摆：连续 idleTimeout 没有任何字节到达。
// 它与 context.Canceled 的区分至关重要——后者意味着暂停或退出，重试逻辑必须放过；
// 停摆是网络或服务器的故障，按普通失败重试。
var ErrStalled = errors.New("服务器停止发送数据")

// statusError 是服务器返回 4xx/5xx 的错误。4xx 大多是防盗链令牌失效或地址过期，
// 重试三次也不会变；fetchSegment 对它们不再重试，尽快把真实原因亮给用户。
type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string { return "服务器返回 " + e.status }

// withRequestTimeout 给一次 HLS 请求套上单次超时。调用方负责在函数体里用它返回的 ctx
// 发起请求并读完全部响应体——超时一旦到期，ctx 取消会同时打断读取中的 body。
func withRequestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, requestTimeout)
}

// SetRequestTimeoutForTest 供测试把单次请求超时调到毫秒级（否则「服务器停住」的用例
// 要等 45 秒）。返回恢复函数，测试用 defer 恢复。
func SetRequestTimeoutForTest(d time.Duration) (restore func()) {
	old := requestTimeout
	requestTimeout = d
	return func() { requestTimeout = old }
}

// IdleTimeout 返回当前的空闲判定上限。测试用它保存旧值，以便恢复。
func IdleTimeout() time.Duration { return idleTimeout }

// SetIdleTimeoutForTest 供测试把空闲判定调到毫秒级。
func SetIdleTimeoutForTest(d time.Duration) { idleTimeout = d }

// idleReader 包住响应体，两次读到字节之间的间隔超过空闲上限时取消请求并标记停摆。
// 「慢但活跃」的响应（限速服务器）继续传输，只有真正停住的连接被斩断。
type idleReader struct {
	r     io.Reader
	timer *time.Timer
	fired atomic.Bool
}

// newIdleReader 挂上一个空闲看门狗。cancel 是这次请求超时 ctx 的取消函数：
// 空闲到期时取消它，阻塞中的 body 读取立刻以错误返回。
func newIdleReader(r io.Reader, cancel context.CancelFunc) *idleReader {
	ir := &idleReader{r: r}
	ir.timer = time.AfterFunc(idleTimeout, func() {
		ir.fired.Store(true)
		cancel()
	})
	return ir
}

func (ir *idleReader) Read(p []byte) (int, error) {
	// Reset 与看门狗回调分属两个 goroutine，极端交错时可能提前取消一次请求；
	// 那只是多走一遍分片重试，不影响正确性，为此加锁不值得。
	ir.timer.Reset(idleTimeout)
	return ir.r.Read(p)
}

// Stalled 报告这次响应是否因空闲而被斩断。读到错误后立即查询，据此换用 ErrStalled。
func (ir *idleReader) Stalled() bool { return ir.fired.Load() }

func (ir *idleReader) stop() {
	ir.timer.Stop()
}

// Inputs 是一次传输已经落盘的输入：初始化片段与按播放顺序排列的媒体分片。
//
// 存的是分片目录内的文件名而不是绝对路径：这份输入要随任务一起持久化，重启、换临时目录、
// 换机器之后仍然对得上（见 Paths）。
type Inputs struct {
	Init     []string `json:"init,omitempty"`
	Segments []string `json:"segments"`
}

// Paths 把输入展开成 Media Processor 需要的绝对路径。
func (in *Inputs) Paths(dir string) (initPaths, segmentPaths []string) {
	if in == nil {
		return nil, nil
	}
	for _, name := range in.Init {
		initPaths = append(initPaths, filepath.Join(dir, name))
	}
	for _, name := range in.Segments {
		segmentPaths = append(segmentPaths, filepath.Join(dir, name))
	}
	return initPaths, segmentPaths
}

// ProgressFunc 汇报分片下载进度。Total 为 -1 表示总大小未知。done 是整条清单的分片
// 完成位图快照（索引即分片序号；调用方可以改写自己的副本，不会影响下载过程），
// 续传时一开始就带出磁盘上已有的分片，调用方靠它把「分段并发」画进界面。
type ProgressFunc func(downloaded, total int64, completed, totalCount int, done []bool)

// Fetcher 负责抓取清单、密钥与分片，始终带着任务的请求上下文（Referer/Cookie 等）。
type Fetcher struct {
	client  *http.Client
	creds   credentials.RequestCredentials
	userAgt string
	mu      sync.Mutex
	keys    map[string][]byte
}

// NewFetcher 构造一个属于某次任务的抓取器。creds 是这次请求的上下文（Referer/Cookie 等），
// 清单、密钥与分片都必须带着它——需要登录或防盗链的站点的分片只有带上才取得到。
func NewFetcher(client *http.Client, creds credentials.RequestCredentials) *Fetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return &Fetcher{
		client:  client,
		creds:   creds,
		userAgt: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		keys:    make(map[string][]byte),
	}
}

// GetText 抓取一份文本清单（主清单或媒体清单）。
func (f *Fetcher) GetText(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := withRequestTimeout(ctx)
	defer cancel()
	resp, err := f.do(ctx, rawURL, 0, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// Download 把媒体清单里的全部分片下载到 dir，返回交给 Media Processor 的输入。
//
// 断点续传按分片粒度实现：分片先写 .part 再改名，因此「目标文件存在」等价于
// 「这个分片已完整落盘」。暂停后继续、处理失败重试、重启后继续都只补缺失的分片。
func (f *Fetcher) Download(ctx context.Context, pl *MediaPlaylist, dir string, concurrency int, onProgress ProgressFunc) (*Inputs, error) {
	return f.DownloadWithPrefix(ctx, pl, dir, "", concurrency, onProgress)
}

// DownloadWithPrefix 支持带文件名前缀下载分片（如音视频分离时使用 "video" / "audio" 前缀区分落盘文件）。
func (f *Fetcher) DownloadWithPrefix(ctx context.Context, pl *MediaPlaylist, dir string, prefix string, concurrency int, onProgress ProgressFunc) (*Inputs, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建分片临时目录: %w", err)
	}
	if concurrency <= 0 {
		concurrency = defaultSegmentConcurrency
	}
	if concurrency > len(pl.Segments) {
		concurrency = len(pl.Segments)
	}

	inputs, err := f.planInputs(ctx, pl, dir, prefix)
	if err != nil {
		return nil, err
	}

	total := pl.KnownSize()

	// 已完成的分片（续传时已在磁盘上）直接计入进度起点。
	var mu sync.Mutex
	downloaded := int64(0)
	completed := 0
	doneFlags := make([]bool, len(pl.Segments))
	for i, name := range inputs.Segments {
		if info, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
			downloaded += info.Size()
			completed++
			doneFlags[i] = true
		}
	}
	report := func() {
		if onProgress != nil {
			snapshot := make([]bool, len(doneFlags))
			copy(snapshot, doneFlags)
			onProgress(downloaded, total, completed, len(pl.Segments), snapshot)
		}
	}
	report()

	var firstErr error
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, seg := range pl.Segments {
		dest := filepath.Join(dir, inputs.Segments[i])
		if _, statErr := os.Stat(dest); statErr == nil {
			continue
		}
		wg.Add(1)
		go func(seg Segment, dest string, index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}
			written, fetchErr := f.fetchSegment(ctx, seg, dest)

			mu.Lock()
			defer mu.Unlock()
			if fetchErr != nil {
				if firstErr == nil && !errors.Is(fetchErr, context.Canceled) {
					firstErr = fetchErr
				}
				return
			}
			downloaded += written
			doneFlags[index] = true
			completed++
			report()
		}(seg, dest, i)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return inputs, ctx.Err()
	}
	if firstErr != nil {
		return inputs, firstErr
	}
	return inputs, nil
}

// planInputs 定下每个分片在磁盘上的落点，并按需下载初始化片段。
// 分片文件名带序号，与清单顺序一一对应，续传因此不必额外记录已完成列表。
func (f *Fetcher) planInputs(ctx context.Context, pl *MediaPlaylist, dir string, prefix string) (*Inputs, error) {
	inputs := &Inputs{}

	var initSection *InitSection
	for _, seg := range pl.Segments {
		if seg.Map == nil {
			continue
		}
		if initSection == nil {
			initSection = seg.Map
			continue
		}
		if seg.Map.URI != initSection.URI {
			// 同一份成品中途更换初始化片段意味着编码参数改变，属于首期明确不支持的结构。
			return nil, fmt.Errorf("清单在中途更换初始化片段（EXT-X-MAP），暂不支持拼成一份成品")
		}
	}

	for i, seg := range pl.Segments {
		inputs.Segments = append(inputs.Segments, segmentName(prefix, i, seg.URI))
	}

	if initSection != nil {
		name := initName(prefix, initSection.URI)
		initPath := filepath.Join(dir, name)
		if _, err := os.Stat(initPath); err != nil {
			if _, err := f.fetchSegment(ctx, segmentFromInit(initSection), initPath); err != nil {
				return nil, fmt.Errorf("下载初始化片段失败: %w", err)
			}
		}
		inputs.Init = append(inputs.Init, name)
	}
	return inputs, nil
}

func segmentFromInit(init *InitSection) Segment {
	return Segment{
		URI:         init.URI,
		RangeStart:  init.RangeStart,
		RangeLength: init.RangeLength,
	}
}

func segmentName(prefix string, index int, rawURL string) string {
	if prefix != "" {
		return fmt.Sprintf("%s_seg_%05d%s", prefix, index, segmentExt(rawURL))
	}
	return fmt.Sprintf("seg_%05d%s", index, segmentExt(rawURL))
}

func initName(prefix, rawURL string) string {
	if prefix != "" {
		return fmt.Sprintf("%s_init%s", prefix, segmentExt(rawURL))
	}
	return "init" + segmentExt(rawURL)
}

// segmentExt 从分片地址取后缀，只用于让落盘名可读；真正的容器类型由 Media Processor
// 按内容判断，不靠后缀。
func segmentExt(rawURL string) string {
	clean := rawURL
	if idx := strings.IndexAny(clean, "?#"); idx >= 0 {
		clean = clean[:idx]
	}
	ext := path.Ext(clean)
	if ext == "" || len(ext) > 8 || strings.ContainsAny(ext, "/\\") {
		return ".bin"
	}
	return ext
}

// fetchSegment 抓取并解密一个分片，成功时文件已经原子改名到 dest；返回落盘字节数。
func (f *Fetcher) fetchSegment(ctx context.Context, seg Segment, dest string) (int64, error) {
	var lastErr error
	for attempt := 0; attempt < segmentFetchAttempts; attempt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		written, err := f.downloadOnce(ctx, seg, dest)
		if err == nil {
			return written, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("分片 %s 下载失败: %w", seg.URI, lastErr)
}

func (f *Fetcher) downloadOnce(ctx context.Context, seg Segment, dest string) (int64, error) {
	if seg.Key == nil {
		return f.streamToDisk(ctx, seg, dest)
	}
	if !strings.EqualFold(seg.Key.Method, MethodAES128) {
		return 0, fmt.Errorf("不支持的加密方法 %s（首期只支持标准 AES-128）", seg.Key.Method)
	}
	if seg.Key.URI == "" {
		return 0, fmt.Errorf("加密分片缺少密钥地址（EXT-X-KEY 没有 URI）")
	}

	key, err := f.fetchKey(ctx, seg.Key.URI)
	if err != nil {
		return 0, err
	}
	iv := seg.Key.IV
	if len(iv) == 0 {
		iv = SequenceIV(seg.MediaSeq)
	}

	encrypted, err := f.readAll(ctx, seg)
	if err != nil {
		return 0, err
	}
	plain, err := DecryptAES128(encrypted, key, iv)
	if err != nil {
		return 0, err
	}
	if err := atomicfile.Write(dest, plain, 0o644, false); err != nil {
		return 0, err
	}
	return int64(len(plain)), nil
}

// streamToDisk 直接把未加密的分片写进目标文件：不经过内存，大分片也不会占用额外内存。
func (f *Fetcher) streamToDisk(ctx context.Context, seg Segment, dest string) (int64, error) {
	// 超时覆盖从发请求到读完响应体的全程：服务器发出响应头后停住不动是最难缠的
	// 卡死形态，只有把 body 读取也放进同一个期限里才兜得住。空闲看门狗再补一刀：
	// 字节流彻底停止时不必等满总超时。
	ctx, cancel := withRequestTimeout(ctx)
	defer cancel()
	resp, err := f.do(ctx, seg.URI, seg.RangeStart, seg.RangeLength)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body := newIdleReader(resp.Body, cancel)
	defer body.stop()

	partPath := dest + ".part"
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(file, body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(partPath)
		if body.Stalled() {
			return 0, fmt.Errorf("%w（%s 内没有收到任何数据）", ErrStalled, idleTimeout)
		}
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partPath)
		return 0, closeErr
	}
	if seg.RangeLength > 0 && written != seg.RangeLength {
		_ = os.Remove(partPath)
		return 0, fmt.Errorf("分片长度不符：期望 %d 字节，实际 %d 字节", seg.RangeLength, written)
	}
	if err := os.Rename(partPath, dest); err != nil {
		_ = os.Remove(partPath)
		return 0, err
	}
	return written, nil
}

func (f *Fetcher) readAll(ctx context.Context, seg Segment) ([]byte, error) {
	ctx, cancel := withRequestTimeout(ctx)
	defer cancel()
	resp, err := f.do(ctx, seg.URI, seg.RangeStart, seg.RangeLength)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body := newIdleReader(resp.Body, cancel)
	defer body.stop()
	data, readErr := io.ReadAll(body)
	if readErr != nil && body.Stalled() {
		return nil, fmt.Errorf("%w（%s 内没有收到任何数据）", ErrStalled, idleTimeout)
	}
	return data, readErr
}

// fetchKey 取一个密钥并缓存：同一密钥地址在同一次下载里只取一次。
func (f *Fetcher) fetchKey(ctx context.Context, uri string) ([]byte, error) {
	f.mu.Lock()
	cached, ok := f.keys[uri]
	f.mu.Unlock()
	if ok {
		return cached, nil
	}

	key, err := func() ([]byte, error) {
		ctx, cancel := withRequestTimeout(ctx)
		defer cancel()
		resp, err := f.do(ctx, uri, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("读取 AES-128 密钥失败: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body := newIdleReader(resp.Body, cancel)
		defer body.stop()
		data, readErr := io.ReadAll(body)
		if readErr != nil && body.Stalled() {
			return nil, fmt.Errorf("读取 AES-128 密钥失败: %w", fmt.Errorf("%w（%s 内没有收到任何数据）", ErrStalled, idleTimeout))
		}
		if readErr != nil {
			return nil, fmt.Errorf("读取 AES-128 密钥失败: %w", readErr)
		}
		return data, nil
	}()
	if err != nil {
		return nil, err
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("AES-128 密钥必须是 16 字节，实际 %d 字节", len(key))
	}

	f.mu.Lock()
	f.keys[uri] = key
	f.mu.Unlock()
	return key, nil
}

// do 发起一次带任务请求上下文的 GET；length 大于 0 时按范围取。
func (f *Fetcher) do(ctx context.Context, rawURL string, start, length int64) (*http.Response, error) {
	req, err := f.newRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return nil, err
	}
	if length > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+length-1))
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return nil, &statusError{code: resp.StatusCode, status: resp.Status}
	}
	return resp, nil
}

// newRequest 构造一个带任务请求上下文的请求：User-Agent、Accept 与 creds 是每次抓取
// 都要带的，集中在这里保证 HEAD 探测与分片下载用同一套头，不各写一份再漂移。
func (f *Fetcher) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgt)
	req.Header.Set("Accept", "*/*")
	if f.creds != nil {
		f.creds.ApplyToHTTPRequest(req)
	}
	return req, nil
}
