// Package media 是 ADR-0001 的 Media Processor：把下载好的分片统一承接为一份成品。
//
// 它只做解析、轨道合并、demux、mux 与无损 remux，不做任何重新编码——成品里的每个样本都是
// 来源分片里的原始编码数据，只换了容器与索引。具体媒体库封装在包内，HLS Engine 与任务系统
// 都不直接依赖它们（ADR-0001）。
package media

import (
	"context"
	"fmt"
	"math"
	"os"

	mp4codecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/pmp4"
)

const tsPacketSize = 188

// mediaTimeScale 是成品统一使用的时间基：90 kHz 是 MPEG 系列的原生时间基，
// 视频时间戳可以直接沿用而不丢精度。
const mediaTimeScale = 90000

// audioFrameSamples 是 AAC 一个访问单元的采样数。AAC 的帧长固定，因此音频样本的时长
// 直接由它给出，不必从时间戳差推算——也推不出来，音频的 PTS 是按帧给出的。
const audioFrameSamples = 1024

// defaultVideoFrameRate 是推不出帧时长时的兜底帧率。只有视频轨在整段里都找不到可参照的
// 时间戳差时才会用到；不给兜底会在 stts 里写出零时长样本，播放器会把整条轨判成损坏。
const defaultVideoFrameRate = 25

// maxSingleFileBytes 是单文件 MP4 的上限。封装的 box 长度与样本偏移都是 32 位，
// 超过这个规模会静默溢出成一份坏文件，因此在写之前就明确拒绝。
const maxSingleFileBytes = int64(1)<<32 - 1<<20

type containerKind int

const (
	containerUnknown containerKind = iota
	containerTS
	containerFMP4
)

// Input 是一次处理要读的媒体文件。Parts 是按播放顺序排列的媒体分片，
// Init 是 fMP4 的初始化片段（EXT-X-MAP），TS 下载时为空。
type Input struct {
	Init    []string
	Parts   []string
	Output  string
	WorkDir string
}

// Process 把分片处理成一份成品，写到 Output。
// 失败时保留输入分片，由调用方决定是仅重试处理还是重新传输（ADR-0004）。
func Process(ctx context.Context, in Input) error {
	if len(in.Parts) == 0 {
		return unsupportedf("没有可处理的媒体分片")
	}

	store, err := newSampleStore(in.WorkDir)
	if err != nil {
		return err
	}
	defer store.close()

	acc := &accumulator{store: store}

	for _, path := range in.Init {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := acc.addInit(path); err != nil {
			return err
		}
	}
	for _, path := range in.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := acc.addPart(path); err != nil {
			return err
		}
	}

	if err := acc.finalize(); err != nil {
		return err
	}
	return acc.write(in.Output)
}

type trackKind int

const (
	kindVideo trackKind = iota
	kindAudio
)

func (k trackKind) String() string {
	if k == kindVideo {
		return "视频"
	}
	return "音频"
}

// videoCodecKind 是视频轨的编码种类。TS 分片里没有容器级的编码声明，只能靠解封装层
// 给出的轨道类型判断；参数集收集齐之后才能构造出写进成品的码点。
type videoCodecKind int

const (
	videoCodecUnknown videoCodecKind = iota
	videoCodecH264
	videoCodecH265
)

// sample 是一条轨道里的一个样本。pts/dts 是来源时间基下的原始时间戳，
// duration/ptsOffset 在 finalize 阶段按目标时间基填好。
type sample struct {
	pts       int64
	dts       int64
	nonSync   bool
	offset    int64
	size      int
	duration  uint32
	ptsOffset int32
}

// track 是累积出来的一条轨道。
type track struct {
	kind      trackKind
	name      string
	timeScale uint32
	startTime int64
	offset    int64
	samples   []sample

	// durationsFromContainer 表示样本时长由容器直接给出（fMP4 的 trun 带每样本时长）。
	// TS 分片没有这个信息，视频时长要自己用相邻样本的解码时间差补出来。
	durationsFromContainer bool

	// 编解码参数：TS 从码流里收集，fMP4 从初始化片段里取。
	videoCodec    videoCodecKind
	sps, pps, vps []byte
	mp4Codec      mp4codecs.Codec
}

type accumulator struct {
	store  *sampleStore
	kind   containerKind
	tracks []*track
	// initTracks 是 fMP4 初始化片段声明的轨道，按轨道 ID 索引。
	initTracks map[int]*track
}

func (a *accumulator) addInit(path string) error {
	if a.kind == containerTS {
		return unsupportedf("MPEG-TS 下载不应带初始化片段")
	}
	a.kind = containerFMP4
	return a.readFMP4Init(path)
}

// addPart 按内容分派解封装。一份成品里的所有分片必须是同一种容器，
// 混用意味着拼出来的索引必然对不上，因此直接报不支持。
func (a *accumulator) addPart(path string) error {
	kind, err := sniffContainer(path)
	if err != nil {
		return err
	}
	if a.kind == containerUnknown {
		a.kind = kind
	} else if a.kind != kind {
		return unsupportedf("分片容器不一致：同一份成品里既有 %s 又有 %s，暂不支持拼接", containerName(a.kind), containerName(kind))
	}

	switch kind {
	case containerTS:
		return a.readTSFile(path)
	case containerFMP4:
		return a.readFMP4Part(path)
	}
	return unsupportedf("无法识别的媒体结构: %s", path)
}

func containerName(k containerKind) string {
	switch k {
	case containerTS:
		return "MPEG-TS"
	case containerFMP4:
		return "fMP4"
	}
	return "未知容器"
}

// trackFor 取某一类轨道，没有就建一条。同一类出现第二路轨道属于首期不支持的结构
// （多路视频、多音轨），由调用方在发现时明确报错。
func (a *accumulator) trackFor(kind trackKind, name string) *track {
	for _, t := range a.tracks {
		if t.kind == kind {
			return t
		}
	}
	t := &track{kind: kind, name: name}
	a.tracks = append(a.tracks, t)
	return t
}

// finalize 计算时间基准与每个样本的时长、呈现偏移。
//
// 各轨道的起始时间不同（音轨通常比画面早一点），成品以所有轨道里最早的那个时刻为 0 点，
// 其余轨道用一个起始偏移对上，这样打开就是同步的。
func (a *accumulator) finalize() error {
	if len(a.tracks) == 0 {
		return unsupportedf("没有解析出可用的音视频轨")
	}

	origin := int64(math.MaxInt64)
	for _, t := range a.tracks {
		if t.timeScale == 0 {
			return unsupportedf("%s 缺少时间基", t.name)
		}
		if len(t.samples) == 0 {
			return unsupportedf("%s 没有任何样本", t.name)
		}
		if start := to90k(t.startTime, t.timeScale); start < origin {
			origin = start
		}
	}
	for _, t := range a.tracks {
		t.offset = (to90k(t.startTime, t.timeScale) - origin) * int64(t.timeScale) / mediaTimeScale
		if err := t.finalizeSamples(); err != nil {
			return err
		}
		if err := t.ensureMP4Codec(); err != nil {
			return err
		}
	}

	if a.store.size >= maxSingleFileBytes {
		return unsupportedf("成品超过 4 GB，暂不支持写出单个 MP4 文件")
	}
	return nil
}

func (t *track) finalizeSamples() error {
	if !t.durationsFromContainer {
		if t.kind == kindAudio {
			for i := range t.samples {
				t.samples[i].duration = audioFrameSamples
			}
		} else {
			t.deriveVideoDurations()
		}
	}

	fallback := t.timeScale / defaultVideoFrameRate
	if fallback == 0 {
		fallback = 1
	}
	for i := range t.samples {
		if t.samples[i].duration == 0 {
			t.samples[i].duration = fallback
		}
		offset := t.samples[i].pts - t.samples[i].dts
		if offset > math.MaxInt32 || offset < math.MinInt32 {
			return unsupportedf("%s 的呈现时间与解码时间差距过大，暂不支持", t.name)
		}
		t.samples[i].ptsOffset = int32(offset)
	}
	return nil
}

// deriveVideoDurations 用相邻样本的解码时间差补出帧时长。
func (t *track) deriveVideoDurations() {
	n := len(t.samples)
	for i := 0; i+1 < n; i++ {
		if delta := t.samples[i+1].dts - t.samples[i].dts; delta > 0 && delta < 1<<32 {
			t.samples[i].duration = uint32(delta)
		}
	}
	// 最后一个样本的真实时长取决于不在本次输入里的下一帧，沿用前一个已知值。
	if n >= 2 {
		t.samples[n-1].duration = t.samples[n-2].duration
	}
	// 分片边界上时间戳不连续时上面的差会落空，从后往前把缺口补齐。
	var known uint32
	for i := n - 1; i >= 0; i-- {
		if t.samples[i].duration == 0 {
			t.samples[i].duration = known
			continue
		}
		known = t.samples[i].duration
	}
}

// write 把累积的轨道写成一份渐进式 MP4（ftyp + moov + mdat），
// 先写临时文件再改名，半成品不会被当成已完成的成品。
func (a *accumulator) write(output string) error {
	presentation := &pmp4.Presentation{}
	for i, t := range a.tracks {
		mt := &pmp4.Track{
			ID:         i + 1,
			TimeScale:  t.timeScale,
			TimeOffset: int32(t.offset),
			Codec:      t.mp4Codec,
		}
		for _, s := range t.samples {
			mt.Samples = append(mt.Samples, &pmp4.Sample{
				Duration:        s.duration,
				PTSOffset:       s.ptsOffset,
				IsNonSyncSample: s.nonSync,
				PayloadSize:     uint32(s.size),
				GetPayload:      a.store.reader(s.offset, s.size),
			})
		}
		presentation.Tracks = append(presentation.Tracks, mt)
	}

	partPath := output + ".sheepget"
	file, err := os.Create(partPath)
	if err != nil {
		return fmt.Errorf("无法创建成品文件: %w", err)
	}
	if err := presentation.Marshal(file); err != nil {
		_ = file.Close()
		_ = os.Remove(partPath)
		return fmt.Errorf("封装成品失败: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("写入成品失败: %w", err)
	}
	if err := os.Rename(partPath, output); err != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("无法把成品移动到 %s: %w", output, err)
	}
	return nil
}

// to90k 把一个时间戳从 scale 时间基换算到 90 kHz。先除后乘，避免大时间戳溢出。
func to90k(value int64, scale uint32) int64 {
	s := int64(scale)
	if s <= 0 {
		return 0
	}
	return value/s*mediaTimeScale + (value%s)*mediaTimeScale/s
}
