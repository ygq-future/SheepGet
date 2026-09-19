package media

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	mp4codecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// readFMP4Init 解析初始化片段，得到轨道的时间基与编解码参数。
// fMP4 的媒体分片里只有样本，没有这些声明，因此初始化片段是必需输入。
func (a *accumulator) readFMP4Init(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	var init fmp4.Init
	if err := init.Unmarshal(file); err != nil {
		return unsupportedf("%s 不是可解析的 fMP4 初始化片段: %v", filepath.Base(path), err)
	}
	if len(init.Tracks) == 0 {
		return unsupportedf("%s 里没有声明任何媒体轨", filepath.Base(path))
	}

	seen := map[trackKind]bool{}
	if a.initTracks == nil {
		a.initTracks = make(map[int]*track)
	}
	for _, it := range init.Tracks {
		kind, name, err := fmp4CodecKind(it.Codec)
		if err != nil {
			return err
		}
		if seen[kind] {
			return unsupportedf("初始化片段里有多路%s轨，暂不支持拼成一份成品", kind)
		}
		seen[kind] = true

		if it.TimeScale == 0 {
			return unsupportedf("%s 缺少时间基", name)
		}
		t := a.trackFor(kind, name)
		if t.timeScale != 0 && t.timeScale != it.TimeScale {
			return unsupportedf("%s 的时间基前后不一致（%d / %d）", name, t.timeScale, it.TimeScale)
		}
		t.timeScale = it.TimeScale
		t.mp4Codec = it.Codec
		t.durationsFromContainer = true
		a.initTracks[it.ID] = t
	}
	return nil
}

// readFMP4Part 解析一个 fMP4 媒体分片。样本负载原样取出：fMP4 里的样本本来就是
// 长度前缀格式，与成品需要的完全一致，因此这里没有任何转码或重排。
func (a *accumulator) readFMP4Part(path string) error {
	if a.initTracks == nil {
		return unsupportedf("%s 是 fMP4 分片，但缺少初始化片段（EXT-X-MAP）", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var parts fmp4.Parts
	if err := parts.Unmarshal(data); err != nil {
		return unsupportedf("%s 不是可解析的 fMP4 分片: %v", filepath.Base(path), err)
	}
	if len(parts) == 0 {
		return unsupportedf("%s 里没有媒体数据", filepath.Base(path))
	}

	for _, part := range parts {
		for _, pt := range part.Tracks {
			t := a.initTracks[pt.ID]
			if t == nil {
				return unsupportedf("%s 引用了初始化片段里没有声明的轨道 %d", filepath.Base(path), pt.ID)
			}
			time := int64(pt.BaseTime)
			for _, s := range pt.Samples {
				pts := time + int64(s.PTSOffset)
				if err := a.appendSample(t, sample{
					pts:      pts,
					dts:      time,
					nonSync:  s.IsNonSyncSample,
					duration: s.Duration,
				}, s.Payload); err != nil {
					return err
				}
				time += int64(s.Duration)
			}
		}
	}
	return nil
}

func fmp4CodecKind(codec mp4codecs.Codec) (trackKind, string, error) {
	switch codec.(type) {
	case *mp4codecs.H264:
		return kindVideo, "H.264 视频轨", nil
	case *mp4codecs.H265:
		return kindVideo, "H.265 视频轨", nil
	case *mp4codecs.MPEG4Audio:
		return kindAudio, "AAC 音频轨", nil
	}
	return kindVideo, "", unsupportedf("fMP4 初始化片段里的编码 %s 不在首期支持范围内（支持 H.264/H.265 + AAC）", describeCodec(codec))
}

func describeCodec(codec mp4codecs.Codec) string {
	switch codec.(type) {
	case *mp4codecs.AV1:
		return "AV1"
	case *mp4codecs.VP9:
		return "VP9"
	case *mp4codecs.MPEG4Video:
		return "MPEG-4 视频"
	case *mp4codecs.MPEG1Video:
		return "MPEG-1/2 视频"
	case *mp4codecs.MJPEG:
		return "M-JPEG"
	case *mp4codecs.Opus:
		return "Opus 音频"
	case *mp4codecs.AC3:
		return "AC-3 音频"
	case *mp4codecs.EAC3:
		return "E-AC-3 音频"
	case *mp4codecs.MPEG1Audio:
		return "MPEG-1/2 音频"
	case *mp4codecs.FLAC:
		return "FLAC 音频"
	case *mp4codecs.LPCM:
		return "LPCM 音频"
	}
	return fmt.Sprintf("%T", codec)
}
