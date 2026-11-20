package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/asticode/go-astits"
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

	// 解封装库只把每个 ES 归到码点，不暴露 PMT 里的 stream_type 与描述符。识别
	// Apple HLS 的 timed ID3 元数据轨需要在 PES 级别看 PMT 声明，因此遇到无法归类
	// 的轨时用 astits 单独读一遍 PMT。读不出来时保持原有行为：真正不支持的轨仍然
	// 明确报错；全是受支持码点时则完全不做这次额外解析。
	seen := map[trackKind]bool{}
	var pmtStreams map[uint16]*astits.PMTElementaryStream
	var pmtLoaded bool
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
			// 分片里常带一条 timed ID3 元数据轨（Apple 的 HLS 时间戳就是放在这里）。
			// 成品只承载音视频，元数据轨直接跳过；其它不支持的码点仍然明确报错。
			if !pmtLoaded {
				pmtLoaded = true
				pmtStreams, err = readPMTStreams(path)
				if err != nil {
					return fmt.Errorf("读取 %s 的节目表失败: %w", filepath.Base(path), err)
				}
			}
			if isTimedID3Metadata(pmtStreams[src.PID]) {
				continue
			}
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

const (
	// metadataDescriptorTag 是 ISO/IEC 13818-1 的 metadata_descriptor。
	metadataDescriptorTag = 0x26
	// id3FormatIdentifier 是 metadata_descriptor 里用来声明 timed ID3 的四字符标识。
	id3FormatIdentifier = "ID3 "
	// id3FormatIdentifierCode 是同一个标识在 registration_descriptor 里的 32 位编码。
	id3FormatIdentifierCode uint32 = 0x49443320
)

// readPMTStreams 用 astits 读出 PMT 的全部基本流声明。
//
// 只用来识别解封装库没有归类的轨：媒体验证与样本解析仍然走解封装库，
// 这里不消费 PES，也不改变任何解析结果。
func readPMTStreams(path string) (map[uint16]*astits.PMTElementaryStream, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	demuxer := astits.NewDemuxer(context.Background(), file)
	for {
		data, nextErr := demuxer.NextData()
		if nextErr != nil {
			return nil, nextErr
		}
		if data == nil {
			break
		}
		if data.PMT == nil {
			continue
		}
		streams := make(map[uint16]*astits.PMTElementaryStream, len(data.PMT.ElementaryStreams))
		for _, es := range data.PMT.ElementaryStreams {
			streams[es.ElementaryPID] = es
		}
		if len(streams) > 0 {
			return streams, nil
		}
	}
	return nil, fmt.Errorf("PMT 里没有基本流声明")
}

// isTimedID3Metadata 判断一个 PMT 基本流是不是 Apple HLS 的 timed ID3 元数据轨。
//
// 这类轨用 stream_type 0x15（Metadata）声明，并带 metadata_descriptor，
// 其 metadata_format_identifier 为 "ID3 "；部分封装器改用 registration_descriptor
// 声明同一个标识，也一并接受。只有确认是 ID3 元数据才跳过，其它不支持的码点
// 仍按原样报错，避免把真正的音视频轨静默丢掉。
func isTimedID3Metadata(es *astits.PMTElementaryStream) bool {
	if es == nil {
		return false
	}
	switch es.StreamType {
	case astits.StreamTypeMetadata, astits.StreamTypePrivateData:
	default:
		return false
	}

	for _, d := range es.ElementaryStreamDescriptors {
		if d == nil {
			continue
		}
		switch d.Tag {
		case metadataDescriptorTag:
			if d.Unknown != nil && hasID3MetadataFormatIdentifier(d.Unknown.Content) {
				return true
			}
		case astits.DescriptorTagRegistration:
			if d.Registration != nil && d.Registration.FormatIdentifier == id3FormatIdentifierCode {
				return true
			}
		}
	}
	return false
}

// hasID3MetadataFormatIdentifier 解析 metadata_descriptor 的内容（不含 tag/length），
// 检查两处可选的格式标识里是否出现 "ID3 "。
func hasID3MetadataFormatIdentifier(content []byte) bool {
	if len(content) < 2 {
		return false
	}
	pos := 2
	if binary.BigEndian.Uint16(content[:2]) == 0xFFFF {
		if len(content) < pos+4 {
			return false
		}
		if string(content[pos:pos+4]) == id3FormatIdentifier {
			return true
		}
		pos += 4
	}
	if len(content) < pos+1 || content[pos] != 0xFF {
		return false
	}
	pos++
	if len(content) < pos+4 {
		return false
	}
	return string(content[pos:pos+4]) == id3FormatIdentifier
}

// mpegtsCodecName 把一个解封装库没有归类成功的码点翻译成可读名称。
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
