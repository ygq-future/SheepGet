package media_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"
	mpegtscodecs "github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/pmp4"

	"sheep-get/internal/media"
)

const (
	sampleH264AAC = "testdata/h264_aac.ts"
	sampleH265AAC = "testdata/h265_aac.ts"
	sampleVideo   = "testdata/video_h264.ts"
	sampleAudio   = "testdata/audio_aac.ts"
	sampleFMP4Ini = "testdata/fmp4_init.mp4"
	sampleFMP4A   = "testdata/fmp4_seg0.m4s"
	sampleFMP4B   = "testdata/fmp4_seg1.m4s"
)

func process(t *testing.T, parts []string, init ...string) (string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.mp4")
	err := media.Process(context.Background(), media.Input{
		Init:    init,
		Parts:   parts,
		Output:  out,
		WorkDir: t.TempDir(),
	})
	return out, err
}

func mustProcess(t *testing.T, parts []string, init ...string) string {
	t.Helper()
	out, err := process(t, parts, init...)
	if err != nil {
		t.Fatalf("Process 失败: %v", err)
	}
	if info, statErr := os.Stat(out); statErr != nil || info.Size() == 0 {
		t.Fatalf("成品没有落盘: %v", statErr)
	}
	return out
}

// decodeStreams 用 ffprobe 独立读出成品的轨道。它是与封装库完全无关的一条检查路径：
// 只知道「文件能不能被业界工具认成这两条轨」。
func decodeStreams(t *testing.T, path string) []string {
	t.Helper()
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("本机没有 ffprobe，跳过独立解析校验")
	}
	out, err := exec.Command(ffprobe, "-v", "error",
		"-show_entries", "stream=codec_name,codec_type",
		"-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe 读不出成品: %v", err)
	}
	var streams []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			streams = append(streams, line)
		}
	}
	return streams
}

// decodeCleanly 让 ffmpeg 真正解码成品的每一条轨。封装正确但样本被破坏时，只有解码才会暴露。
func decodeCleanly(t *testing.T, path string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机没有 ffmpeg，跳过解码校验")
	}
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", path, "-f", "null", "-")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ffmpeg 解码成品失败: %v\n%s", err, stderr.String())
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		t.Fatalf("ffmpeg 解码成品时报错:\n%s", msg)
	}
}

func TestProcessMPEGTSH264AndAAC(t *testing.T) {
	out := mustProcess(t, []string{sampleH264AAC})

	streams := decodeStreams(t, out)
	if len(streams) != 2 {
		t.Fatalf("期望两条轨（视频 + 音频），实际 %v", streams)
	}
	if streams[0] != "h264,video" || streams[1] != "aac,audio" {
		t.Fatalf("轨道不对: %v", streams)
	}
	decodeCleanly(t, out)
}

func TestProcessMPEGTSH265AndAAC(t *testing.T) {
	out := mustProcess(t, []string{sampleH265AAC})

	streams := decodeStreams(t, out)
	if len(streams) != 2 {
		t.Fatalf("期望两条轨（视频 + 音频），实际 %v", streams)
	}
	if streams[0] != "hevc,video" || streams[1] != "aac,audio" {
		t.Fatalf("轨道不对: %v", streams)
	}
	decodeCleanly(t, out)
}

func TestProcessSingleTrackSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sample string
		want   string
	}{
		{"只有画面", sampleVideo, "h264,video"},
		{"只有声音", sampleAudio, "aac,audio"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := mustProcess(t, []string{tc.sample})
			streams := decodeStreams(t, out)
			if len(streams) != 1 || streams[0] != tc.want {
				t.Fatalf("期望 %q 一条轨，实际 %v", tc.want, streams)
			}
			decodeCleanly(t, out)
		})
	}
}

// TestProcessFMP4WithInit 覆盖 fMP4 分片：样本时长由 trun 直接给出，与 TS 靠时间戳差推算
// 是两条不同的路径。
func TestProcessFMP4WithInit(t *testing.T) {
	out := mustProcess(t, []string{sampleFMP4A, sampleFMP4B}, sampleFMP4Ini)

	streams := decodeStreams(t, out)
	if len(streams) != 2 {
		t.Fatalf("期望两条轨，实际 %v", streams)
	}
	decodeCleanly(t, out)
}

// TestProcessIsLossless 逐样本比对成品与来源码流：成品里的每一段负载都必须与解封装拿到的
// 原始访问单元逐字节相同，样本数一致，顺序一致。这是「只换容器与索引、不重新编码」的直接证据，
// 也不依赖任何外部工具。
func TestProcessIsLossless(t *testing.T) {
	want := readTSAccessUnits(t, sampleVideo)
	if len(want) == 0 {
		t.Fatal("来源样本里没有解析出访问单元")
	}

	out := mustProcess(t, []string{sampleVideo})
	got := readPM4SamplePayloads(t, out)

	if len(got) != len(want) {
		t.Fatalf("样本数不一致：来源 %d，成品 %d", len(want), len(got))
	}
	for i := range want {
		if string(got[i]) != string(want[i]) {
			t.Fatalf("第 %d 个样本与来源不一致（来源 %d 字节，成品 %d 字节）",
				i, len(want[i]), len(got[i]))
		}
	}
}

// TestProcessAccumulatesParts 覆盖多分片拼接：同一份分片重复两次，样本数应当翻倍——
// 这同时说明跨分片的时间戳是接着排的，而不是各自从 0 重来。
func TestProcessAccumulatesParts(t *testing.T) {
	single := readPM4SamplePayloads(t, mustProcess(t, []string{sampleVideo}))
	double := readPM4SamplePayloads(t, mustProcess(t, []string{sampleVideo, sampleVideo}))

	if len(double) != 2*len(single) {
		t.Fatalf("期望样本数翻倍：单片 %d，双片 %d", len(single), len(double))
	}
	for i := range single {
		if string(double[i]) != string(single[i]) || string(double[i+len(single)]) != string(single[i]) {
			t.Fatalf("第 %d 个样本在两份分片里不一致", i)
		}
	}
}

func TestProcessRejectsMixedContainers(t *testing.T) {
	_, err := process(t, []string{sampleVideo, sampleFMP4A}, sampleFMP4Ini)
	if !media.IsUnsupported(err) {
		t.Fatalf("期望「不支持」错误，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "容器") {
		t.Fatalf("错误没有说明容器不一致: %v", err)
	}
}

func TestProcessRejectsUnrecognizableInput(t *testing.T) {
	junk := filepath.Join(t.TempDir(), "junk.ts")
	if err := os.WriteFile(junk, make([]byte, 4096), 0o644); err != nil {
		t.Fatalf("无法写出测试输入: %v", err)
	}

	_, err := process(t, []string{junk})
	if !media.IsUnsupported(err) {
		t.Fatalf("期望「不支持」错误，实际 %v", err)
	}
}

func TestProcessRequiresInitForFMP4Part(t *testing.T) {
	_, err := process(t, []string{sampleFMP4A})
	if !media.IsUnsupported(err) {
		t.Fatalf("期望「不支持」错误，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "初始化片段") {
		t.Fatalf("错误没有说明缺少初始化片段: %v", err)
	}
}

func TestProcessRejectsEmptyInput(t *testing.T) {
	_, err := process(t, nil)
	if !media.IsUnsupported(err) {
		t.Fatalf("期望「不支持」错误，实际 %v", err)
	}
}

func TestIsUnsupportedIgnoresOtherErrors(t *testing.T) {
	if media.IsUnsupported(errors.New("普通错误")) {
		t.Fatal("普通错误不应被当成「不支持」")
	}
	if media.IsUnsupported(nil) {
		t.Fatal("nil 不应被当成「不支持」")
	}
}

// readTSAccessUnits 独立解封装一份 MPEG-TS，按解码顺序取出各访问单元的 AVCC 字节。
func readTSAccessUnits(t *testing.T, path string) [][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("无法打开样本 %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	reader := &mpegts.Reader{R: file}
	if err := reader.Initialize(); err != nil {
		t.Fatalf("样本不是可解析的 MPEG-TS: %v", err)
	}

	var units [][]byte
	for _, src := range reader.Tracks() {
		if _, ok := src.Codec.(*mpegtscodecs.H264); ok {
			reader.OnDataH264(src, func(_, _ int64, au [][]byte) error {
				payload, _ := h264.AVCC(au).Marshal()
				units = append(units, payload)
				return nil
			})
		}
	}
	reader.OnDecodeError(func(error) {})
	for {
		if err := reader.Read(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("解析样本失败: %v", err)
		}
	}
	return units
}

// readPM4SamplePayloads 从成品里按轨道顺序取回全部样本负载。
func readPM4SamplePayloads(t *testing.T, path string) [][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("无法打开成品: %v", err)
	}
	defer func() { _ = file.Close() }()

	var presentation pmp4.Presentation
	if err := presentation.Unmarshal(file); err != nil {
		t.Fatalf("成品不是可解析的 MP4: %v", err)
	}
	if len(presentation.Tracks) == 0 {
		t.Fatal("成品里没有任何轨道")
	}

	var payloads [][]byte
	for _, tr := range presentation.Tracks[0].Samples {
		payload, payloadErr := tr.GetPayload()
		if payloadErr != nil {
			t.Fatalf("读不出样本负载: %v", payloadErr)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}
