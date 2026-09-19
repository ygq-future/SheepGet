package media

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	mp4codecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"
	mpegtscodecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts/codecs"
)

// readTSFile 解封装一个 MPEG-TS 分片，把音视频访问单元累积到对应轨道上。
//
// 每条轨道在每个分片里都各自带一遍参数集（SPS/PPS 通常在每个 IDR 前重复），
// 因此跨分片只按「视频/音频」归类，不按 PID：PID 在不同分片之间并不保证一致，
// 而按归类拼出来的轨道才是连续的。
func (a *accumulator) readTSFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	reader := &mpegts.Reader{R: file}
	if err := reader.Initialize(); err != nil {
		return unsupportedf("%s 不是可解析的 MPEG-TS 分片: %v", filepath.Base(path), err)
	}

	srcTracks := reader.Tracks()
	if len(srcTracks) == 0 {
		return unsupportedf("%s 里没有声明任何媒体轨", filepath.Base(path))
	}

	seen := map[trackKind]bool{}
	for _, src := range srcTracks {
		switch codec := src.Codec.(type) {
		case *mpegtscodecs.H264:
			dst, err := a.bindTSVideoTrack(kindVideo, "H.264 视频轨", videoCodecH264, seen)
			if err != nil {
				return err
			}
			reader.OnDataH264(src, func(pts, dts int64, au [][]byte) error {
				return a.addH264Sample(dst, pts, dts, au)
			})

		case *mpegtscodecs.H265:
			dst, err := a.bindTSVideoTrack(kindVideo, "H.265 视频轨", videoCodecH265, seen)
			if err != nil {
				return err
			}
			reader.OnDataH265(src, func(pts, dts int64, au [][]byte) error {
				return a.addH265Sample(dst, pts, dts, au)
			})

		case *mpegtscodecs.MPEG4Audio:
			if codec.SampleRate <= 0 {
				return unsupportedf("%s 的 AAC 轨缺少采样率", filepath.Base(path))
			}
			dst, err := a.bindTSAudioTrack(codec, seen)
			if err != nil {
				return err
			}
			reader.OnDataMPEG4Audio(src, func(pts int64, aus [][]byte) error {
				return a.addAACFrames(dst, pts, aus)
			})

		default:
			return unsupportedf("%s 里的 %s 不在首期支持范围内（支持 H.264/H.265 + AAC）",
				filepath.Base(path), mpegtsCodecName(src.Codec))
		}
	}

	reader.OnDecodeError(func(error) {})
	for {
		if readErr := reader.Read(); readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("%s 解析中断: %w", filepath.Base(path), readErr)
		}
	}
	return nil
}

// bindTSVideoTrack 认领这一类视频轨。一个分片里出现第二路视频意味着多画面/多节目，
// 首期不做，明确报不支持而不是悄悄丢掉一路。
func (a *accumulator) bindTSVideoTrack(kind trackKind, name string, vc videoCodecKind, seen map[trackKind]bool) (*track, error) {
	if seen[kind] {
		return nil, unsupportedf("分片里有多路视频轨，暂不支持拼成一份成品")
	}
	seen[kind] = true
	t := a.trackFor(kind, name)
	if t.timeScale == 0 {
		t.timeScale = mediaTimeScale
	}
	t.videoCodec = vc
	return t, nil
}

func (a *accumulator) bindTSAudioTrack(codec *mpegtscodecs.MPEG4Audio, seen map[trackKind]bool) (*track, error) {
	if seen[kindAudio] {
		return nil, unsupportedf("分片里有多路音频轨，暂不支持拼成一份成品")
	}
	seen[kindAudio] = true
	t := a.trackFor(kindAudio, "AAC 音频轨")
	rate := uint32(codec.SampleRate)
	if t.timeScale != 0 && t.timeScale != rate {
		return nil, unsupportedf("音频轨的采样率前后不一致（%d / %d）", t.timeScale, rate)
	}
	t.timeScale = rate
	t.mp4Codec = &mp4codecs.MPEG4Audio{Config: codec.Config}
	return t, nil
}

func (a *accumulator) addH264Sample(t *track, pts, dts int64, au [][]byte) error {
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h264.NALUType(nalu[0] & 0x1f) {
		case h264.NALUTypeSPS:
			if len(t.sps) == 0 {
				t.sps = append([]byte(nil), nalu...)
			}
		case h264.NALUTypePPS:
			if len(t.pps) == 0 {
				t.pps = append([]byte(nil), nalu...)
			}
		}
	}
	payload, err := h264.AVCC(au).Marshal()
	if err != nil {
		return unsupportedf("%s 的访问单元无法转换: %v", t.name, err)
	}
	return a.appendSample(t, sample{pts: pts, dts: dts, nonSync: !h264.IsRandomAccess(au)}, payload)
}

func (a *accumulator) addH265Sample(t *track, pts, dts int64, au [][]byte) error {
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h265.NALUType(nalu[0] >> 1) {
		case h265.NALUType_VPS_NUT:
			if len(t.vps) == 0 {
				t.vps = append([]byte(nil), nalu...)
			}
		case h265.NALUType_SPS_NUT:
			if len(t.sps) == 0 {
				t.sps = append([]byte(nil), nalu...)
			}
		case h265.NALUType_PPS_NUT:
			if len(t.pps) == 0 {
				t.pps = append([]byte(nil), nalu...)
			}
		}
	}
	payload, err := h264.AVCC(au).Marshal()
	if err != nil {
		return unsupportedf("%s 的访问单元无法转换: %v", t.name, err)
	}
	return a.appendSample(t, sample{pts: pts, dts: dts, nonSync: !h265.IsRandomAccess(au)}, payload)
}

// addAACFrames 处理一个 PES 里的若干 AAC 帧。ADTS 头已由解封装层去掉，
// 每帧固定 1024 个采样，因此只取 PES 的 PTS 作为该组帧的起点。
func (a *accumulator) addAACFrames(t *track, pts int64, aus [][]byte) error {
	if t.timeScale == 0 {
		return unsupportedf("%s 缺少时间基", t.name)
	}
	base := from90k(pts, t.timeScale)
	for i, au := range aus {
		time := base + int64(i*audioFrameSamples)
		if err := a.appendSample(t, sample{pts: time, dts: time}, au); err != nil {
			return err
		}
	}
	return nil
}

// appendSample 把一段负载写进样本临时文件，并把它登记到轨道上。
func (a *accumulator) appendSample(t *track, s sample, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	offset, size, err := a.store.append(payload)
	if err != nil {
		return err
	}
	s.offset, s.size = offset, size
	if len(t.samples) == 0 {
		t.startTime = s.dts
	}
	t.samples = append(t.samples, s)
	return nil
}

// ensureMP4Codec 按收集到的参数集构造视频码点。参数集缺失说明这份流不是完整可用的
// H.264/H.265，明确报不支持而不是写出一份播放器打不开的成品。
func (t *track) ensureMP4Codec() error {
	if t.mp4Codec != nil {
		return nil
	}
	switch t.videoCodec {
	case videoCodecH264:
		if len(t.sps) == 0 || len(t.pps) == 0 {
			return unsupportedf("%s 缺少 SPS/PPS 参数集", t.name)
		}
		t.mp4Codec = &mp4codecs.H264{SPS: t.sps, PPS: t.pps}
	case videoCodecH265:
		if len(t.vps) == 0 || len(t.sps) == 0 || len(t.pps) == 0 {
			return unsupportedf("%s 缺少 VPS/SPS/PPS 参数集", t.name)
		}
		t.mp4Codec = &mp4codecs.H265{VPS: t.vps, SPS: t.sps, PPS: t.pps}
	default:
		return unsupportedf("%s 缺少可写入成品的编解码参数", t.name)
	}
	return nil
}

func mpegtsCodecName(codec mpegtscodecs.Codec) string {
	switch codec.(type) {
	case *mpegtscodecs.MPEG4AudioLATM:
		return "AAC-LATM 音频"
	case *mpegtscodecs.MPEG1Audio:
		return "MPEG-1/2 音频"
	case *mpegtscodecs.MPEG1Video:
		return "MPEG-1/2 视频"
	case *mpegtscodecs.MPEG4Video:
		return "MPEG-4 视频"
	case *mpegtscodecs.AC3:
		return "AC-3 音频"
	case *mpegtscodecs.EAC3:
		return "E-AC-3 音频"
	case *mpegtscodecs.Opus:
		return "Opus 音频"
	case *mpegtscodecs.DVBSubtitle:
		return "DVB 字幕"
	case *mpegtscodecs.KLV:
		return "KLV 元数据"
	}
	return "未知编码"
}

// from90k 把一个 90 kHz 时间戳换算到目标时间基。
func from90k(value int64, scale uint32) int64 {
	return value/mediaTimeScale*int64(scale) + (value%mediaTimeScale)*int64(scale)/mediaTimeScale
}
