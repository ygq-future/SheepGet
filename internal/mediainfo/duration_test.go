package mediainfo

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// sliceSource 是「整个文件都在手边」的 Source：本地文件解析走的就是这种形态。
type sliceSource []byte

func (s sliceSource) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= int64(len(s)) {
		return 0, io.EOF
	}
	n := copy(p, s[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s sliceSource) Size() int64 { return int64(len(s)) }

func box(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

// mvhdV0 是 version 0 的 mvhd：version/flags(4) creation(4) modification(4) timescale(4) duration(4)。
func mvhdV0(timeScale, duration uint32) []byte {
	body := make([]byte, 20)
	binary.BigEndian.PutUint32(body[12:16], timeScale)
	binary.BigEndian.PutUint32(body[16:20], duration)
	return box("mvhd", body)
}

// mvhdV1 是 version 1 的 mvhd：时间戳与时长都变成 64 位。
func mvhdV1(timeScale uint32, duration uint64) []byte {
	body := make([]byte, 32)
	body[0] = 1
	binary.BigEndian.PutUint32(body[20:24], timeScale)
	binary.BigEndian.PutUint64(body[24:32], duration)
	return box("mvhd", body)
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ftyp 用真实的 box 结构占位，让解析器像在真文件里一样从偏移 0 顺序走下来。
func ftyp() []byte { return box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2")) }

func mdat(size int) []byte {
	payload := make([]byte, size)
	return box("mdat", payload)
}

func TestMP4DurationFromHead(t *testing.T) {
	file := concat(ftyp(), box("moov", mvhdV0(1000, 185000)), mdat(64))

	seconds, ok := FromSource("movie.mp4", sliceSource(file))
	if !ok {
		t.Fatal("expected a duration from the head of a faststart mp4")
	}
	if math.Abs(seconds-185) > 0.001 {
		t.Errorf("duration = %v, want 185", seconds)
	}
}

func TestMP4DurationVersion1(t *testing.T) {
	file := concat(ftyp(), box("moov", mvhdV1(90000, 90000*42)))

	seconds, ok := FromSource("clip.mov", sliceSource(file))
	if !ok {
		t.Fatal("expected a duration from a version 1 mvhd")
	}
	if math.Abs(seconds-42) > 0.001 {
		t.Errorf("duration = %v, want 42", seconds)
	}
}

// 未做 faststart 的 mp4 把 moov 放在文件尾：头部那一段里走不到它，只能从尾部片段找回来。
func TestMP4DurationFromTail(t *testing.T) {
	// mdat 比头部读取窗口更大，头部片段里就再也走不到 moov，正是真实文件的情形。
	file := concat(ftyp(), mdat(1<<20), box("moov", mvhdV0(600, 600*90)))

	seconds, ok := FromSource("big.mp4", sliceSource(file))
	if !ok {
		t.Fatal("expected a duration found in the tail of a non-faststart mp4")
	}
	if math.Abs(seconds-90) > 0.001 {
		t.Errorf("duration = %v, want 90", seconds)
	}
}

// mdat 的载荷里可能恰好出现 "moov" 四个字节，不能就此认定找到了 moov。
func TestMP4TailScanIgnoresMoovInsidePayload(t *testing.T) {
	trap := box("mdat", concat([]byte("....moov"), []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01}, make([]byte, 512)))
	file := concat(ftyp(), mdat(1<<20), trap, box("moov", mvhdV0(1000, 5000)))

	seconds, ok := FromSource("trap.mp4", sliceSource(file))
	if !ok {
		t.Fatal("expected the real moov to be found after the decoy bytes")
	}
	if math.Abs(seconds-5) > 0.001 {
		t.Errorf("duration = %v, want 5", seconds)
	}
}

func TestMP4UnknownDurationMarkerIsRejected(t *testing.T) {
	// duration 全 1 是「未知」的写法，换算出来是个荒谬的值，不能当成时长报出去。
	file := concat(ftyp(), box("moov", mvhdV0(1000, math.MaxUint32)))
	if _, ok := FromSource("unknown.mp4", sliceSource(file)); ok {
		t.Error("expected the 'unknown duration' marker to be rejected")
	}
}

// --- Matroska / WebM ---

var (
	ebmlHeaderID = []byte{0x1A, 0x45, 0xDF, 0xA3}
	segmentID    = []byte{0x18, 0x53, 0x80, 0x67}
	infoID       = []byte{0x15, 0x49, 0xA9, 0x66}
	timecodeID   = []byte{0x2A, 0xD7, 0xB1}
	durationID   = []byte{0x44, 0x89}
)

// ebmlSize 按 EBML 规则编码元素长度（保留前缀标记位）。
func ebmlSize(value int) []byte {
	switch {
	case value < 0x7F:
		return []byte{0x80 | byte(value)}
	case value < 0x3FFF:
		return []byte{0x40 | byte(value>>8), byte(value)}
	case value < 0x1FFFFF:
		return []byte{0x20 | byte(value>>16), byte(value >> 8), byte(value)}
	default:
		return []byte{0x10 | byte(value>>24), byte(value >> 16), byte(value >> 8), byte(value)}
	}
}

func element(id, payload []byte) []byte {
	return concat(id, ebmlSize(len(payload)), payload)
}

// unknownSizeElement 写「长度未知」的花括号：0x01 后面跟全 1，直播流的 Segment 常这么写。
func unknownSizeElement(id, payload []byte) []byte {
	return concat(id, []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, payload)
}

func float32Bytes(value float32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, math.Float32bits(value))
	return out
}

func webmFile(timecodeScale uint32, durationMs float32) []byte {
	info := element(infoID, concat(
		element(timecodeID, []byte{byte(timecodeScale >> 16), byte(timecodeScale >> 8), byte(timecodeScale)}),
		element(durationID, float32Bytes(durationMs)),
	))
	return concat(element(ebmlHeaderID, []byte("webm")), unknownSizeElement(segmentID, info))
}

func TestMatroskaDuration(t *testing.T) {
	// 200000 个 tick，每 tick 1ms → 200 秒
	file := webmFile(1_000_000, 200000)

	seconds, ok := FromSource("clip.webm", sliceSource(file))
	if !ok {
		t.Fatal("expected a duration from a webm header")
	}
	if math.Abs(seconds-200) > 0.001 {
		t.Errorf("duration = %v, want 200", seconds)
	}

	// mkvc 也可以没有 TimecodeScale：默认 1ms
	seconds, ok = FromSource("clip.mkv", sliceSource(concat(
		element(ebmlHeaderID, []byte("matroska")),
		unknownSizeElement(segmentID, element(infoID, element(durationID, float32Bytes(95000)))),
	)))
	if !ok {
		t.Fatal("expected a duration with the default timecode scale")
	}
	if math.Abs(seconds-95) > 0.001 {
		t.Errorf("duration = %v, want 95", seconds)
	}
}

// SeekHead 里记着 Info 元素的 ID，但它不是 Info 元素本身；解析必须跳过这类假线索。
func TestMatroskaSkipsSeekHeadOccurrence(t *testing.T) {
	seekHead := element([]byte{0x11, 0x4D, 0x9B, 0x74}, element([]byte{0x4D, 0xBB}, concat(
		element([]byte{0x53, 0xAB}, infoID),             // SeekID = Info 的 ID
		element([]byte{0x53, 0xAC}, []byte{0x00, 0x40}), // SeekPosition
	)))
	info := element(infoID, concat(
		element(timecodeID, []byte{0x0F, 0x42, 0x40}),
		element(durationID, float32Bytes(63000)),
	))
	file := concat(
		element(ebmlHeaderID, []byte("matroska")),
		unknownSizeElement(segmentID, concat(seekHead, info)),
	)

	seconds, ok := FromSource("seekhead.webm", sliceSource(file))
	if !ok {
		t.Fatal("expected the real Info element to be used after the SeekHead decoy")
	}
	if math.Abs(seconds-63) > 0.001 {
		t.Errorf("duration = %v, want 63", seconds)
	}
}

func TestUnsupportedAndBrokenInputs(t *testing.T) {
	if Supported("movie.ts") || Supported("archive.zip") || Supported("noext") {
		t.Error("only the containers we can read should be reported as supported")
	}
	if _, ok := FromSource("movie.ts", sliceSource(concat(ftyp(), box("moov", mvhdV0(1000, 1000))))); ok {
		t.Error("an unsupported container must not report a duration")
	}
	if _, ok := FromSource("broken.mp4", sliceSource([]byte{0x00, 0x00, 0x00})); ok {
		t.Error("a truncated file must not report a duration")
	}
	if _, ok := FromSource("empty.mp4", sliceSource(nil)); ok {
		t.Error("an empty file must not report a duration")
	}
}

func TestFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, concat(ftyp(), box("moov", mvhdV0(1000, 125000)), mdat(32)), 0o644); err != nil {
		t.Fatalf("failed to write sample: %v", err)
	}

	seconds, ok := FromFile(path)
	if !ok {
		t.Fatal("expected a duration from a local file")
	}
	if math.Abs(seconds-125) > 0.001 {
		t.Errorf("duration = %v, want 125", seconds)
	}

	if _, ok := FromFile(filepath.Join(dir, "missing.mp4")); ok {
		t.Error("a missing file must not report a duration")
	}
	if _, ok := FromFile(dir); ok {
		t.Error("a directory must not report a duration")
	}
}
