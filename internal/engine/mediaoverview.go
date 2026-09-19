package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"sheep-get/internal/credentials"
	"sheep-get/internal/hls"
)

// mediaOverviewBudget 是一次概览探测愿意花的总预算。它服务的是扩展面板上的一行展示
// 信息（时长与大小），探不出来就按未知显示，绝不能拖住面板。
const mediaOverviewBudget = 10 * time.Second

// MediaOverview 是「下载之前就能知道」的媒体展示信息。
type MediaOverview struct {
	// DurationSeconds 是媒体时长（秒）。清单声明的时长、或直链媒体的容器解析结果。
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	// TotalBytes 是这次下载的总大小。HLS 来自 BYTERANGE 清单或逐分片询问；直链由
	// 扩展在嗅探时从 Content-Length 得到，这里只在清单探测成功时给出 HLS 的值。
	TotalBytes int64 `json:"totalBytes,omitempty"`
	// Variants 大于 1 表示这份清单有多个清晰度。时长与大小此时一并不给：
	// 不同清晰度的大小各不相同，必须先选定一个才有「这次下载的大小」可言。
	Variants int `json:"variants,omitempty"`
}

// ProbeMediaOverview 为扩展面板探测一条链接的展示信息（时长与大小）。
// 探测结果只用于显示，不进入任务：真正的建任务路径仍然走自己的探测与选择流程。
func (m *Manager) ProbeMediaOverview(ctx context.Context, urlStr, filename, mimeType string, isHls bool, totalBytes int64, headers map[string]string) (*MediaOverview, error) {
	ctx, cancel := context.WithTimeout(ctx, mediaOverviewBudget)
	defer cancel()

	if isHls || looksLikePlaylist(urlStr, mimeType) {
		return m.probeHLSOverview(ctx, urlStr, credentials.New(headers))
	}

	// 直链媒体：大小扩展在嗅探时已经从响应头拿到（这里原样带回），时长走容器解析——
	// 它按 Range 预算读文件的头部与尾部，不下载整个文件。
	ov := &MediaOverview{TotalBytes: totalBytes}
	if seconds, ok := m.ProbeMediaDuration(ctx, urlStr, filename, totalBytes, headers); ok {
		ov.DurationSeconds = seconds
	}
	return ov, nil
}

// probeHLSOverview 读一份清单，给出选定唯一清晰度后的时长与大小。
// 多清晰度的清单没有「唯一的大小」，只回报清晰度数量，选择仍由悬浮条或文件信息窗口完成。
func (m *Manager) probeHLSOverview(ctx context.Context, playlistURL string, creds credentials.RequestCredentials) (*MediaOverview, error) {
	if strings.TrimSpace(playlistURL) == "" {
		return nil, errors.New("缺少 HLS 清单地址")
	}
	f := m.newHLSFetcher(creds)
	res, err := hls.Inspect(ctx, f, playlistURL)
	if err != nil {
		return nil, err
	}
	if len(res.Variants) > 1 {
		return &MediaOverview{Variants: len(res.Variants)}, nil
	}

	variant := res.Variants[0]
	sel, err := hls.Resolve(ctx, f, playlistURL, variant, hls.PickAudioURI(res.Media, variant.AudioGroupID))
	if err != nil {
		return nil, err
	}
	ov := &MediaOverview{DurationSeconds: sel.Source.Duration, TotalBytes: sel.Source.TotalBytes}
	if ov.TotalBytes <= 0 {
		// BYTERANGE 之外清单不写分片长度；预算内问得到就给，问不全按未知显示。
		if size := m.probeSelectionSize(ctx, f, sel); size > 0 {
			ov.TotalBytes = size
		}
	}
	return ov, nil
}
