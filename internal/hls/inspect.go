package hls

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// maxSizeProbeSegments 是为「显示一个大小」愿意发出的最大请求数。分片长度不在清单里，
// 只能逐个问服务器；超出这个数量就不问了，界面按未知大小显示——宁可显示未知，
// 也不为一个展示用的数字打几百次请求。
const maxSizeProbeSegments = 200

// sizeProbeConcurrency 是大小探测的并发数。
const sizeProbeConcurrency = 8

// Source 是一次 HLS 下载选定后的事实。它既足以重新取回清单（后端的续传与重试都靠它），
// 也足以在界面上说明这次下载（清晰度、时长、分片数）。
type Source struct {
	PlaylistURL string  `json:"playlistUrl"`
	Variant     Variant `json:"variant"`
	// AudioURI 是独立音轨的媒体清单地址；音视频分离的清单必须一并下载才能拼出完整成品。
	AudioURI string `json:"audioUri,omitempty"`
	// Duration 是清单声明的总时长（秒）。它是可获得的已知信息，界面据此展示。
	Duration float64 `json:"duration,omitempty"`
	Segments int     `json:"segments,omitempty"`
	// TotalBytes 是这次下载的总大小；-1 表示还没确定，界面按未知显示。
	// 分片长度通常不在清单里（除非用了 BYTERANGE），因此它多半要靠逐分片询问才能得到。
	TotalBytes int64 `json:"totalBytes,omitempty"`
}

// Selection 是选定一个清晰度之后的完整事实：要落进任务的部分，加上本次探测顺手取回的
// 分片清单（供大小探测复用，不进入任务记录）。
type Selection struct {
	Source        *Source
	VideoPlaylist *MediaPlaylist
	AudioPlaylist *MediaPlaylist
}

// InspectResult 是清单探测结果：可选的清晰度列表。
type InspectResult struct {
	Kind Kind
	// Variants 是可选清晰度。媒体清单直接给时只有一项，也就是它自己。
	Variants []Variant
	// Media 是主清单声明的关联音轨，用于把独立音轨与所选清晰度对上（PickAudioURI）。
	Media []MediaEntry
}

// Inspect 读取清单并列出可选清晰度。多清晰度的选择据此进行；只有一项时不构成选择。
func Inspect(ctx context.Context, f *Fetcher, playlistURL string) (*InspectResult, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, fmt.Errorf("无效的清单地址: %w", err)
	}
	body, err := f.GetText(ctx, playlistURL)
	if err != nil {
		return nil, fmt.Errorf("读取 HLS 清单失败: %w", err)
	}
	parsed, err := Parse(base, body)
	if err != nil {
		return nil, err
	}

	switch parsed.Kind {
	case KindMaster:
		return &InspectResult{Kind: KindMaster, Variants: parsed.Master.Variants, Media: parsed.Master.Media}, nil
	default:
		// 直接给出的就是媒体清单：它自己就是唯一可下载的版本。
		return &InspectResult{Kind: KindMedia, Variants: []Variant{{URI: playlistURL}}}, nil
	}
}

// Resolve 读取选中清晰度的媒体清单，得到要落进任务的事实；音视频分离的清单同时带上独立音轨。
func Resolve(ctx context.Context, f *Fetcher, playlistURL string, variant Variant, audioURI string) (*Selection, error) {
	video, err := fetchMediaPlaylist(ctx, f, variant.URI)
	if err != nil {
		return nil, fmt.Errorf("读取所选清晰度的清单失败: %w", err)
	}

	source := &Source{
		PlaylistURL: playlistURL,
		Variant:     variant,
		Duration:    video.Duration,
		Segments:    len(video.Segments),
		// 清单自己能给出大小的只有 BYTERANGE 一种情形；给不出来就是未知，不猜。
		TotalBytes: video.KnownSize(),
	}
	sel := &Selection{Source: source, VideoPlaylist: video}

	if audioURI != "" {
		audio, audioErr := fetchMediaPlaylist(ctx, f, audioURI)
		if audioErr != nil {
			return nil, fmt.Errorf("读取独立音轨清单失败: %w", audioErr)
		}
		source.AudioURI = audioURI
		source.Segments += len(audio.Segments)
		source.TotalBytes = sumKnownSizes(source.TotalBytes, audio.KnownSize())
		// 成品时长由最长的一条轨道决定；音轨通常与画面等长，取较大者是安全的。
		if audio.Duration > source.Duration {
			source.Duration = audio.Duration
		}
		sel.AudioPlaylist = audio
	}
	return sel, nil
}

// sumKnownSizes 把两段已知大小加起来；任一段未知（<=0）时整体按未知处理。
func sumKnownSizes(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return -1
	}
	return a + b
}

func fetchMediaPlaylist(ctx context.Context, f *Fetcher, playlistURL string) (*MediaPlaylist, error) {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, fmt.Errorf("无效的清单地址: %w", err)
	}
	body, err := f.GetText(ctx, playlistURL)
	if err != nil {
		return nil, err
	}
	parsed, err := Parse(base, body)
	if err != nil {
		return nil, err
	}
	if parsed.Kind != KindMedia {
		return nil, fmt.Errorf("所选清晰度指向的还是主清单，无法确定播放版本")
	}
	return parsed.Media, nil
}

// PickAudioURI 从主清单里挑出与变体关联的独立音轨清单，没有则为空。
// 同一分组里有多个音轨时取标记为默认的那一个；一个都没标就取第一个。
func PickAudioURI(entries []MediaEntry, groupID string) string {
	if groupID == "" {
		return ""
	}
	var fallback string
	for _, e := range entries {
		if !strings.EqualFold(e.Type, "AUDIO") || e.GroupID != groupID || e.URI == "" {
			continue
		}
		if e.Default {
			return e.URI
		}
		if fallback == "" {
			fallback = e.URI
		}
	}
	return fallback
}

// ProbeSize 用 HEAD 累加分片长度，得到这次下载的总大小；任一分片给不出长度就返回 -1。
//
// 分片长度不在清单里（除非清单用了 BYTERANGE），因此这是唯一能给出真实大小的办法；
// 拿不全就老实报未知，不做按码率推算这类编造。
func (f *Fetcher) ProbeSize(ctx context.Context, playlists []*MediaPlaylist, maxSegments int) int64 {
	if maxSegments <= 0 {
		maxSegments = maxSizeProbeSegments
	}
	var segments []Segment
	for _, pl := range playlists {
		if pl == nil {
			continue
		}
		segments = append(segments, pl.Segments...)
	}
	if len(segments) == 0 || len(segments) > maxSegments {
		return -1
	}

	lengths := make([]int64, len(segments))
	ok := make([]bool, len(segments))
	sem := make(chan struct{}, sizeProbeConcurrency)
	var wg sync.WaitGroup

	for i, seg := range segments {
		wg.Add(1)
		go func(i int, seg Segment) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			size, err := f.headContentLength(ctx, seg)
			if err != nil || size <= 0 {
				return
			}
			lengths[i] = size
			ok[i] = true
		}(i, seg)
	}
	wg.Wait()

	var total int64
	for i, size := range lengths {
		if !ok[i] {
			return -1
		}
		total += size
	}
	return total
}

func (f *Fetcher) headContentLength(ctx context.Context, seg Segment) (int64, error) {
	// 大小探测是逐分片 HEAD，单个挂住就少算一路；超时让它按失败退出，
	// ProbeSize 拿不到全部分片的长度时整体按未知大小显示（宁可未知，不挂死）。
	ctx, cancel := withRequestTimeout(ctx)
	defer cancel()
	req, err := f.newRequest(ctx, http.MethodHead, seg.URI)
	if err != nil {
		return 0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return 0, &statusError{code: resp.StatusCode, status: resp.Status}
	}
	if seg.RangeLength > 0 {
		return seg.RangeLength, nil
	}
	return strconv.ParseInt(strings.TrimSpace(resp.Header.Get("Content-Length")), 10, 64)
}
