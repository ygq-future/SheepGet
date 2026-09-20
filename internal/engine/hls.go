package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/hls"
	"sheep-get/internal/media"
	"sheep-get/internal/task"
)

// MediaOutputExt 是 HLS 成品的后缀。清单本身不是成品，成品是 Media Processor 输出的 MP4，
// 因此建议文件名一律改用它——沿用 .m3u8 只会让用户存下一个名字与内容不符的文件。
const MediaOutputExt = ".mp4"

// hlsSizeProbeTimeout 是解析清晰度时为「显示一个大小」愿意等待的上限。它在用户选定清晰度之后、
// 打开文件信息对话框之前跑，所以可以等一会儿，但不能没有上限。
const hlsSizeProbeTimeout = 5 * time.Second

// hlsPlaylistExtensions 与 hlsPlaylistContentTypes 判定「这个链接指向一份 HLS 清单」。
// 与扩展侧 extension/lib/media.ts 的 HLS_EXTENSIONS / HLS_MIME_PATTERNS 是同一个口径；
// 两端各自实现是因为运行环境不同（Go 与浏览器），改口径时必须一起改。
var hlsPlaylistExtensions = []string{".m3u8", ".m3u"}

var hlsPlaylistContentTypes = map[string]bool{
	"application/vnd.apple.mpegurl": true,
	"application/x-mpegurl":         true,
	"application/octet-stream-m3u8": true,
	"audio/mpegurl":                 true,
	"audio/x-mpegurl":               true,
	"video/mpegurl":                 true,
	"video/x-mpegurl":               true,
}

// HLSProbe 是一条链接的 HLS 事实。
type HLSProbe struct {
	IsHLS bool `json:"isHls"`
	// PlaylistURL 是清单地址，原样保留（可能带查询串，取回清单必须与用户给的一致）。
	PlaylistURL string `json:"playlistUrl,omitempty"`
	// Variants 是可选清晰度。只有一项时不构成选择，界面不必弹选择步骤。
	Variants []hls.Variant `json:"variants,omitempty"`
	// Options 是 Variants 的界面形态：展示名只在后端算一次（hls.VariantOptions），
	// 文件信息窗口与扩展悬浮条拿同一份名字，不在前端各算一遍。
	Options []hls.VariantOption `json:"options,omitempty"`
	// Media 是登记前已经选定的来源。为空表示清晰度还没选——多清晰度时界面先选再进来
	// （Ticket 07：多清晰度在进入信息对话框前选择）。有它时建任务直接采用，不再重新探测。
	Media *hls.Source `json:"media,omitempty"`
}

// newHLSFetcher 构造一个属于某次探测或某次任务的 HLS 抓取器。它复用下载器的 HTTP 客户端，
// 因此代理设置对清单与分片同样生效。
func (m *Manager) newHLSFetcher(creds credentials.RequestCredentials) *hls.Fetcher {
	client := http.DefaultClient
	if m.downloader != nil {
		client = m.downloader.GetClientForDownload()
	}
	return hls.NewFetcher(client, creds)
}

// looksLikePlaylist 判断这次探测拿到的响应是不是 HLS 清单。
// 后缀与 Content-Type 任一命中即可：无后缀的清单地址只有 Content-Type 能证明。
func looksLikePlaylist(rawURL, contentType string) bool {
	clean, _, _ := strings.Cut(rawURL, "?")
	lower := strings.ToLower(clean)
	for _, ext := range hlsPlaylistExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	mime := strings.ToLower(contentType)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return hlsPlaylistContentTypes[mime]
}

// inspectPlaylist 在探测阶段读一次清单，给出可选清晰度。它只读清单本身：
// 分片长度要逐个问服务器，那要等清晰度选定之后才算得起（见 ResolveHLS）。
func (m *Manager) inspectPlaylist(ctx context.Context, playlistURL string, creds credentials.RequestCredentials) (*HLSProbe, error) {
	res, err := hls.Inspect(ctx, m.newHLSFetcher(creds), playlistURL)
	if err != nil {
		return nil, err
	}
	return &HLSProbe{
		IsHLS:       true,
		PlaylistURL: playlistURL,
		Variants:    res.Variants,
		Options:     hls.VariantOptions(res.Variants),
	}, nil
}

// HLSVariantOptions 读取一份清单的可选清晰度，整理成带展示名的选项。
//
// 供浏览器扩展的悬浮条在交接前弹清晰度菜单使用：它和文件信息窗口用的是同一份
// hls.VariantOptions，展示名两处一致，不在扩展侧另算一遍。只有一项时不构成选择，
// 返回的也是那一项，扩展据此决定要不要弹菜单。
func (m *Manager) HLSVariantOptions(ctx context.Context, playlistURL string, creds credentials.RequestCredentials) ([]hls.VariantOption, error) {
	if strings.TrimSpace(playlistURL) == "" {
		return nil, errors.New("缺少 HLS 清单地址")
	}
	res, err := hls.Inspect(ctx, m.newHLSFetcher(creds), playlistURL)
	if err != nil {
		return nil, err
	}
	return hls.VariantOptions(res.Variants), nil
}

// ResolveHLSVariant 读取选定清晰度之后的完整事实：清单地址、清晰度、独立音轨、时长、分片数与大小。
//
// 它在选定清晰度的那一刻被调用一次，结果随任务一起持久化——续传、重试与重启都只靠它，
// 不依赖上一次运行留在内存里的分片清单。
func (m *Manager) ResolveHLSVariant(ctx context.Context, playlistURL, variantURI string, headers map[string]string) (*hls.Source, error) {
	return m.resolveHLS(ctx, playlistURL, variantURI, credentials.New(headers))
}

func (m *Manager) resolveHLS(ctx context.Context, playlistURL, variantURI string, creds credentials.RequestCredentials) (*hls.Source, error) {
	if strings.TrimSpace(playlistURL) == "" {
		return nil, errors.New("缺少 HLS 清单地址")
	}
	f := m.newHLSFetcher(creds)
	res, err := hls.Inspect(ctx, f, playlistURL)
	if err != nil {
		return nil, err
	}

	variant, err := pickVariant(res.Variants, variantURI)
	if err != nil {
		return nil, err
	}
	sel, err := hls.Resolve(ctx, f, playlistURL, variant, hls.PickAudioURI(res.Media, variant.AudioGroupID))
	if err != nil {
		return nil, err
	}

	// 分片长度通常不在清单里，只能逐个问服务器。这是唯一能给出真实大小的办法，
	// 问不全就保持清单能给的那个值（多半是未知），不做按码率推算这类编造。
	if size := m.probeSelectionSize(ctx, f, sel); size > 0 {
		sel.Source.TotalBytes = size
	}
	return sel.Source, nil
}

// ApplyHLSSelection 把选定的清晰度落进探测结果，成为这次下载的事实。
//
// 大小取自选定的那一版：清单自己的字节数与这次下载无关。提交路径靠 probe.HLS.Media
// 建出带媒体来源的任务，因此这一步赋值就是「清晰度已经选好」这件事本身。
func ApplyHLSSelection(probe *ProbeResult, src *hls.Source) {
	if probe == nil || src == nil {
		return
	}
	if probe.HLS == nil {
		probe.HLS = &HLSProbe{IsHLS: true, PlaylistURL: probe.URL}
	}
	probe.HLS.Media = src
	probe.TotalBytes = src.TotalBytes
}

// pickVariant 按地址在候选里定位清晰度。没有指定地址时只接受「没有选择可做」的情形：
// 候选恰好一个。多于一个还硬挑一个，等于替用户做了他没做的决定。
func pickVariant(variants []hls.Variant, variantURI string) (hls.Variant, error) {
	if strings.TrimSpace(variantURI) == "" {
		if len(variants) == 1 {
			return variants[0], nil
		}
		return hls.Variant{}, fmt.Errorf("这份清单有 %d 个清晰度，需要先选定一个", len(variants))
	}
	for _, v := range variants {
		if v.URI == variantURI {
			return v, nil
		}
	}
	return hls.Variant{}, fmt.Errorf("所选清晰度不在清单里：%s", variantURI)
}

func (m *Manager) probeSelectionSize(ctx context.Context, f *hls.Fetcher, sel *hls.Selection) int64 {
	ctx, cancel := context.WithTimeout(ctx, hlsSizeProbeTimeout)
	defer cancel()
	return f.ProbeSize(ctx, []*hls.MediaPlaylist{sel.VideoPlaylist, sel.AudioPlaylist}, 0)
}

// HLSOutputName 把清单地址或探测到的文件名换算成成品的建议文件名。
// HLS 的成品是处理出来的 MP4，沿用 .m3u8 只会让用户存下一个名字与内容不符的文件。
func HLSOutputName(rawURL, filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = URLFilename(rawURL)
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" {
		stem = "media"
	}
	return stem + MediaOutputExt
}

// segmentDir 是 HLS 任务分片的落点目录。它由 GetPartPath 派生，于是清理、换临时目录这些
// 既有行为都会一并作用到分片上，不需要再记第二处位置。
func (m *Manager) segmentDir(t *task.Task) string {
	return segmentDirFor(m.GetPartPath(t))
}

// segmentDirFor 就是「分片目录由分片路径派生」这条关系本身。预下载改落点时要用它算出
// 旧、新两个位置，而不能各自重新推一遍。
func segmentDirFor(partPath string) string {
	return partPath + ".segments"
}

// removeSegmentDir 删除任务的分片目录。只在成品生成成功之后调用：处理失败必须保留分片，
// 否则「仅重试处理」就无从谈起（ADR-0004）。
func (m *Manager) removeSegmentDir(t *task.Task) {
	if t == nil {
		return
	}
	_ = os.RemoveAll(m.segmentDir(t))
}

// runHLSTransfer 下载这次任务的全部媒体分片，返回已经落盘的输入。
//
// 断点续传按分片粒度实现（见 hls.Download）：暂停后继续、处理失败重试、重启后继续都只补
// 缺失的分片，已经完整落盘的分片不会再取一遍。
func (m *Manager) runHLSTransfer(ctx context.Context, t *task.Task, onProgress ProgressFunc) (*hls.Inputs, error) {
	if t.Media == nil {
		return nil, errors.New("任务缺少 HLS 来源")
	}
	dir := m.segmentDir(t)
	f := m.newHLSFetcher(t.RequestHeaders.Clone())

	sel, err := hls.Resolve(ctx, f, t.Media.PlaylistURL, t.Media.Variant, t.Media.AudioURI)
	if err != nil {
		return nil, err
	}

	concurrency := t.MaxConcurrency
	if concurrency <= 0 {
		concurrency = m.config.DefaultConnectionsPerTask
	}

	// 音视频分离时分两条清单先后下载，各自的字节数都从 0 起算。任务级的进度必须单调，
	// 否则进度条会往回走；因此这里只报一遍最大值，最后一段音轨下载期间进度停在画面那一侧，
	// 完成时一次性补到 100%。分片计数则跨两条清单累计：界面用「分片 N/M」展示并发的
	// 分片下载，总数是两条清单之和，中途换清单时数字不会先退回再增长。
	// SegmentDone 把每个分片的完成状态按同样顺序拼成一张位图，界面据此把进度条
	// 画成一格一分片的分段条——否则 HLS 只有一根实心条，多路并发完全看不出来。
	totalSegments := segmentCount(sel.VideoPlaylist) + segmentCount(sel.AudioPlaylist)
	segmentDone := make([]bool, totalSegments)
	t.SegmentDone = segmentDone
	var reported int64
	segmentsDone := 0
	offset := 0
	inputs := &hls.Inputs{}
	type streamTarget struct {
		prefix   string
		playlist *hls.MediaPlaylist
	}
	var streams []streamTarget
	hasBoth := sel.VideoPlaylist != nil && sel.AudioPlaylist != nil
	if sel.VideoPlaylist != nil {
		prefix := ""
		if hasBoth {
			prefix = "video"
		}
		streams = append(streams, streamTarget{prefix: prefix, playlist: sel.VideoPlaylist})
	}
	if sel.AudioPlaylist != nil {
		prefix := ""
		if hasBoth {
			prefix = "audio"
		}
		streams = append(streams, streamTarget{prefix: prefix, playlist: sel.AudioPlaylist})
	}
	for _, stream := range streams {
		pl := stream.playlist
		base := offset
		offset += len(pl.Segments)
		part, dlErr := f.DownloadWithPrefix(ctx, pl, dir, stream.prefix, concurrency, func(downloaded, _ int64, completed, _ int, done []bool) {
			// 最多个把一帧的差异，不会出现非法状态；切片头本身不再替换。
			copy(segmentDone[base:base+len(done)], done)
			t.SegmentsTotal = totalSegments
			t.SegmentsDone = segmentsDone + completed
			if downloaded <= reported {
				return
			}
			reported = downloaded
			if onProgress != nil {
				onProgress(reported, completed, reported)
			}
		})
		segmentsDone += len(pl.Segments)
		if part != nil {
			inputs.Init = append(inputs.Init, part.Init...)
			inputs.Segments = append(inputs.Segments, part.Segments...)
		}
		if dlErr != nil {
			return inputs, dlErr
		}
	}
	return inputs, nil
}

// segmentCount 返回清单的分片数。音视频分离时只有一条轨道会被取回，另一条清单是 nil；
// 纯视频/纯音频清单也一样可能缺音轨清单，所以这里对 nil 一律按 0 处理，否则上面的
// totalSegments 求和会空指针解引用。
func segmentCount(pl *hls.MediaPlaylist) int {
	if pl == nil {
		return 0
	}
	return len(pl.Segments)
}

// runMediaProcessing 把已经落盘的分片处理成一份成品。
func (m *Manager) runMediaProcessing(ctx context.Context, t *task.Task) error {
	if t.MediaInputs == nil {
		return errors.New("任务缺少待处理的媒体输入")
	}
	if err := os.MkdirAll(t.Directory, 0o755); err != nil {
		return fmt.Errorf("无法创建保存目录: %w", err)
	}
	dir := m.segmentDir(t)
	initPaths, segmentPaths := t.MediaInputs.Paths(dir)

	err := media.Process(ctx, media.Input{
		Init:    initPaths,
		Parts:   segmentPaths,
		Output:  filepath.Join(t.Directory, t.Filename),
		WorkDir: dir,
	})
	if err != nil && media.IsUnsupported(err) {
		// 不支持的结构要说清楚是什么不支持，而不是笼统的「处理失败」。
		return fmt.Errorf("这份媒体暂不支持处理：%w", err)
	}
	return err
}
