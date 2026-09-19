// Package mediainfo 从媒体容器的头部（必要时加尾部）片段里读出时长这类容器级信息。
//
// 它只读元数据：不解析音视频轨、不重新封装、也不为了拿一个时长去下整个文件。调用方给一个
// 随机读视图，它按需读几段；读不出来就返回 false，不猜数值——界面宁可显示「未知」，
// 也不该显示一个编出来的时长。
package mediainfo

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Source 是解析器眼里的文件：既能按绝对偏移读，也知道自己有多长。
// 远端实现只在解析真正读到的地方取片段，本地实现就是文件本身。
type Source interface {
	io.ReaderAt
	// Size 是总长度；未知时返回 <= 0，此时尾部片段不可用（解析退化为只读头部）。
	Size() int64
}

// 一次最多读多少字节。常见容器的时长元数据都在文件头（faststart 的 mp4、mkv/webm），
// 只有 moov 落在尾部的 mp4 需要再读一段尾巴。两段都拿不到就认了。
const (
	headScanBytes = 256 << 10
	tailScanBytes = 256 << 10
)

// 超过这个长度就不认为是真时长。mvhd 允许把 duration 写成全 1 表示「未知」，
// 那种值换算出来是个天文数字；这里把它和解析出错一起挡掉。
const maxPlausibleSeconds = 30 * 24 * 60 * 60

type container int

const (
	containerUnknown container = iota
	containerMP4
	containerMatroska
)

// Supported 判断这个文件名是不是我们能读出时长的容器。
func Supported(filename string) bool {
	return containerOf(filename) != containerUnknown
}

// FromFile 读本地文件的时长。下载完成后文件就在本地，读它不产生任何网络请求，
// 因此任务列表里的时长走这条路径，而不是回头再问一遍服务器。
func FromFile(path string) (seconds float64, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	return FromSource(path, fileSource{f: f, size: info.Size()})
}

// FromSource 按容器分派解析：名字只用来看容器类型，长度与内容都来自 src。
func FromSource(name string, src Source) (seconds float64, ok bool) {
	switch containerOf(name) {
	case containerMP4:
		return mp4Duration(src)
	case containerMatroska:
		return matroskaDuration(src)
	}
	return 0, false
}

type fileSource struct {
	f    *os.File
	size int64
}

func (s fileSource) ReadAt(p []byte, off int64) (int, error) { return s.f.ReadAt(p, off) }
func (s fileSource) Size() int64                             { return s.size }

func containerOf(name string) container {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	// ISO BMFF 家族共用 moov/mvhd 结构
	case "mp4", "m4v", "m4a", "mov":
		return containerMP4
	// EBML 家族（mkv/webm）共用 Duration + TimecodeScale
	case "mkv", "webm":
		return containerMatroska
	}
	return containerUnknown
}

// plausibleSeconds 把「时长」这个数值本身挡一道：非正数与明显不合理的值都不算识别成功。
func plausibleSeconds(seconds float64) (float64, bool) {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > maxPlausibleSeconds {
		return 0, false
	}
	return seconds, true
}

// readWindow 读文件的一段：从 start 起最多 length 字节。返回实际读到的内容，
// 读不满也算成功（文件更短、或远端片段只给到这么多），够不够由解析器自己判断。
func readWindow(src Source, start, length int64) ([]byte, bool) {
	if length <= 0 || start < 0 {
		return nil, false
	}
	if size := src.Size(); size > 0 && start+length > size {
		length = size - start
	}
	if length <= 0 {
		return nil, false
	}
	buf := make([]byte, length)
	n, err := src.ReadAt(buf, start)
	if n <= 0 {
		return nil, false
	}
	if err != nil && n < len(buf) {
		buf = buf[:n]
	}
	return buf, true
}

// readTail 读文件末尾的一段。总长度未知时返回 false。
func readTail(src Source, length int64) (data []byte, ok bool) {
	size := src.Size()
	if size <= 0 {
		return nil, false
	}
	if length > size {
		length = size
	}
	if length <= 0 {
		return nil, false
	}
	return readWindow(src, size-length, length)
}

// --- ISO BMFF (mp4/m4v/m4a/mov) ---

const boxMoov = "moov"

// mp4Duration 先走文件头那一段；走不到 moov（未 faststart 的文件）再看文件尾那一段。
//
// 解析一律在取回的片段里做，不按 box 逐个读文件：远端实现按「读」发请求，
// 逐个读 box 头会变成「有几个 box 就发几次请求」，而这里只要两段就够。
func mp4Duration(src Source) (float64, bool) {
	if head, ok := readWindow(src, 0, headScanBytes); ok {
		if seconds, ok := moovDurationInBuffer(head); ok {
			return seconds, true
		}
	}
	tail, ok := readTail(src, tailScanBytes)
	if !ok {
		return 0, false
	}
	return tailMoovDuration(tail)
}

// moovDurationInBuffer 从片段开头顺序走顶层 box，遇到 moov 就读它的 mvhd。
// 片段不够走到 moov 时返回 false，交给尾部路径。
func moovDurationInBuffer(buf []byte) (float64, bool) {
	for offset := 0; ; {
		size, typ, body, ok := readBoxHeaderInBuffer(buf, offset)
		if !ok {
			return 0, false
		}
		if typ == boxMoov {
			return mvhdDurationInBuffer(buf, body, offset+size)
		}
		offset += size
	}
}

// readBoxHeaderInBuffer 读片段里的一个 box 头，返回 box 总长、类型与载荷起点。
// 32 位长度与 64 位长度（size==1）在这里归一化；size==0（到文件尾）只能是最后一个 box，
// 出现它就意味着无法再往下走，按读不出来处理。
func readBoxHeaderInBuffer(buf []byte, offset int) (size int, typ string, body int, ok bool) {
	if offset < 0 || offset+8 > len(buf) {
		return 0, "", 0, false
	}
	size = int(binary.BigEndian.Uint32(buf[offset : offset+4]))
	typ = string(buf[offset+4 : offset+8])
	headerLen := 8
	if size == 1 {
		if offset+16 > len(buf) {
			return 0, "", 0, false
		}
		large := binary.BigEndian.Uint64(buf[offset+8 : offset+16])
		if large > math.MaxInt32 {
			return 0, "", 0, false
		}
		size = int(large)
		headerLen = 16
	}
	if size < headerLen {
		return 0, "", 0, false
	}
	return size, typ, offset + headerLen, true
}

// mvhdDurationInBuffer 在 moov 载荷里找 mvhd。它的位置不固定（前面可能有 udta/meta 等），
// 所以按 box 头遍历，遇到它就读 time scale 与 duration。
func mvhdDurationInBuffer(buf []byte, start, end int) (float64, bool) {
	for offset := start; offset < end; {
		size, typ, body, ok := readBoxHeaderInBuffer(buf, offset)
		if !ok {
			return 0, false
		}
		if typ == "mvhd" {
			return parseMvhdInBuffer(buf, body)
		}
		offset += size
	}
	return 0, false
}

// parseMvhdInBuffer 读 mvhd 的 time scale 与 duration。v0 用 32 位时长、v1 用 64 位。
func parseMvhdInBuffer(buf []byte, at int) (float64, bool) {
	if at < 0 || at+20 > len(buf) {
		return 0, false
	}
	if buf[at] == 1 {
		if at+32 > len(buf) {
			return 0, false
		}
		return scaledSeconds(
			binary.BigEndian.Uint64(buf[at+24:at+32]),
			binary.BigEndian.Uint32(buf[at+20:at+24]),
		)
	}
	return scaledSeconds(
		uint64(binary.BigEndian.Uint32(buf[at+16:at+20])),
		binary.BigEndian.Uint32(buf[at+12:at+16]),
	)
}

func scaledSeconds(duration uint64, timeScale uint32) (float64, bool) {
	if timeScale == 0 || duration == 0 {
		return 0, false
	}
	return plausibleSeconds(float64(duration) / float64(timeScale))
}

// tailMoovDuration 处理 moov 落在文件末尾的 mp4（未做 faststart 的常见情况）。
// 这种文件在头部片段里走不到 moov，只能在尾部片段里找它的 box 头：box 头的类型字段紧跟在
// 4 字节长度之后，因此「moov」出现的位置往前 4 字节就是 box 起点。
func tailMoovDuration(tail []byte) (float64, bool) {
	for i := 4; i+8 <= len(tail); i++ {
		if string(tail[i:i+4]) != boxMoov {
			continue
		}
		size := int(binary.BigEndian.Uint32(tail[i-4 : i]))
		if size < 8 {
			continue
		}
		// mdat 的载荷里也可能出现 "moov" 这四个字节，所以要求紧随其后的必须是一个
		// 能解析出 mvhd 的结构；解析不出来就当这里不是真的 moov，继续往后找。
		if seconds, ok := mvhdDurationInBuffer(tail, i+4, i+size-4); ok {
			return seconds, true
		}
	}
	return 0, false
}

// --- Matroska / WebM (EBML) ---

var (
	idInfo          = []byte{0x15, 0x49, 0xA9, 0x66}
	idTimecodeScale = uint64(0x2AD7B1)
	idDuration      = uint64(0x4489)
)

// matroska 的 Duration 以 TimecodeScale（默认 1ms）为单位，换算成秒要乘它再除以 1e9。
const defaultTimecodeScale = 1_000_000

// matroskaDuration 在头部片段里定位 Info 元素，再读它内部的 TimecodeScale 与 Duration。
// 不按元素层级顺序遍历：Segment 的长度常常写成「未知」（直播流），无法靠长度跳过，
// 而 Info 元素本身几乎总是紧跟 Segment 头，按 ID 定位更稳。
func matroskaDuration(src Source) (float64, bool) {
	head, ok := readWindow(src, 0, headScanBytes)
	if !ok {
		return 0, false
	}
	// 一处「Info 的 ID」不代表那就是 Info 元素：SeekHead 里的 SeekID 记的正是元素的 ID，
	// 而它出现在真正的 Info 之前。所以逐个候选试过去，直到有一个能读出时长。
	for from := 0; ; {
		rel := indexOf(head[from:], idInfo)
		if rel < 0 {
			return 0, false
		}
		at := from + rel
		if seconds, ok := durationInsideInfo(head, at+len(idInfo)); ok {
			return seconds, true
		}
		from = at + 1
	}
}

// durationInsideInfo 从 Info 的载荷起点开始读它的子元素，凑齐 TimecodeScale 与 Duration。
func durationInsideInfo(head []byte, pos int) (float64, bool) {
	cursor := &ebmlCursor{data: head, pos: pos}
	size, unknown, ok := cursor.readSize()
	if !ok {
		return 0, false
	}
	if !unknown {
		// Info 的长度已知：把解析范围限定在它自己的载荷内，免得把后面元素的
		// Duration/TimecodeScale 当成它的。
		end := cursor.pos + int(size)
		if end > len(cursor.data) {
			end = len(cursor.data)
		}
		cursor.data = cursor.data[:end]
	}

	timecodeScale := float64(defaultTimecodeScale)
	var (
		duration     float64
		haveDuration bool
	)
	for {
		id, ok := cursor.readID()
		if !ok {
			break
		}
		elementSize, unknown, ok := cursor.readSize()
		if !ok || unknown {
			break
		}
		if id == idTimecodeScale {
			if value, ok := cursor.readUint(elementSize); ok && value > 0 {
				timecodeScale = float64(value)
			}
			continue
		}
		if id == idDuration {
			if value, ok := cursor.readFloat(elementSize); ok {
				duration = value
				haveDuration = true
			}
			continue
		}
		if !cursor.skip(elementSize) {
			break
		}
	}

	if !haveDuration {
		return 0, false
	}
	return plausibleSeconds(duration * timecodeScale / 1e9)
}

// ebmlCursor 在一片字节里按 EBML 元素读下去。
type ebmlCursor struct {
	data []byte
	pos  int
}

// readID 读元素 ID。ID 自带长度标记（第一个 0 位的位置决定字节数），返回值保留标记位，
// 因此可以直接与 0x1549A966 这类常量比较。
func (c *ebmlCursor) readID() (uint64, bool) {
	length := c.prefixLength()
	if length == 0 {
		return 0, false
	}
	var id uint64
	for i := 0; i < length; i++ {
		id = id<<8 | uint64(c.data[c.pos+i])
	}
	c.pos += length
	return id, true
}

// readSize 读元素长度。长度与前缀标记位一起编码，取值时要把标记位抹掉；
// 全 1 表示「未知长度」。
func (c *ebmlCursor) readSize() (size uint64, unknown bool, ok bool) {
	length := c.prefixLength()
	if length == 0 {
		return 0, false, false
	}
	var raw uint64
	for i := 0; i < length; i++ {
		raw = raw<<8 | uint64(c.data[c.pos+i])
	}
	c.pos += length
	size = raw &^ (uint64(1) << (8*length - length))
	unknown = size == (uint64(1)<<(7*length))-1
	return size, unknown, true
}

// prefixLength 返回紧跟在当前位置的 VINT 前缀长度（1~8 字节），0 表示读不了。
func (c *ebmlCursor) prefixLength() int {
	if c.pos < 0 || c.pos >= len(c.data) {
		return 0
	}
	first := c.data[c.pos]
	for length := 1; length <= 8; length++ {
		if first&(0x80>>(length-1)) != 0 {
			if c.pos+length > len(c.data) {
				return 0
			}
			return length
		}
	}
	return 0
}

func (c *ebmlCursor) take(size uint64) ([]byte, bool) {
	if size > uint64(len(c.data)-c.pos) {
		return nil, false
	}
	out := c.data[c.pos : c.pos+int(size)]
	c.pos += int(size)
	return out, true
}

func (c *ebmlCursor) skip(size uint64) bool {
	_, ok := c.take(size)
	return ok
}

// readUint 读无符号整数载荷（1~8 字节，大端）。
func (c *ebmlCursor) readUint(size uint64) (uint64, bool) {
	raw, ok := c.take(size)
	if !ok || len(raw) == 0 || len(raw) > 8 {
		return 0, false
	}
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return value, true
}

// readFloat 读浮点载荷：EBML 的 Duration 是 4 或 8 字节浮点。
func (c *ebmlCursor) readFloat(size uint64) (float64, bool) {
	raw, ok := c.take(size)
	if !ok {
		return 0, false
	}
	switch len(raw) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(raw))), true
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(raw)), true
	}
	return 0, false
}

func indexOf(haystack, needle []byte) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}
