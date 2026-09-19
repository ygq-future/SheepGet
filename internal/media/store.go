package media

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// sampleStore 把样本负载落到处理期的临时文件里，成品封装时再按偏移读回。
//
// 这样做的原因是内存：一份两小时的 1080p 视频有几十万帧、上 GB 负载，全部留在内存里
// 会直接把进程拖垮。负载只写一次、读一次，代价是一次本地磁盘往返；换来的是内存占用
// 与媒体大小无关，只与单个分片有关。
type sampleStore struct {
	file *os.File
	size int64
}

func newSampleStore(workDir string) (*sampleStore, error) {
	if workDir == "" {
		return nil, fmt.Errorf("缺少处理临时目录")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建处理临时目录: %w", err)
	}
	file, err := os.CreateTemp(workDir, "samples-*.bin")
	if err != nil {
		return nil, fmt.Errorf("无法创建样本临时文件: %w", err)
	}
	return &sampleStore{file: file}, nil
}

// append 追加一段负载，返回它的偏移与长度。
func (s *sampleStore) append(payload []byte) (int64, int, error) {
	offset := s.size
	n, err := s.file.WriteAt(payload, offset)
	if err != nil {
		return 0, 0, err
	}
	s.size += int64(n)
	return offset, n, nil
}

// reader 返回按偏移读回负载的函数；封装阶段逐个样本调用它。
func (s *sampleStore) reader(offset int64, size int) func() ([]byte, error) {
	return func() ([]byte, error) {
		buf := make([]byte, size)
		if _, err := s.file.ReadAt(buf, offset); err != nil {
			return nil, err
		}
		return buf, nil
	}
}

func (s *sampleStore) close() {
	if s == nil || s.file == nil {
		return
	}
	name := s.file.Name()
	_ = s.file.Close()
	_ = os.Remove(name)
}

// sniffContainer 按内容判断一个分片是 MPEG-TS 还是 fMP4。
// 后缀不可靠（HLS 分片常见 .ts / .m4s / .mp4 / 无后缀），因此一律看字节。
func sniffContainer(path string) (containerKind, error) {
	file, err := os.Open(path)
	if err != nil {
		return containerUnknown, err
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, tsPacketSize+1)
	n, _ := io.ReadFull(file, head)

	// MPEG-TS：0x47 同步字节，且 188 字节的包长处仍是 0x47。
	if n >= 1 && head[0] == 0x47 && (n < tsPacketSize+1 || head[tsPacketSize] == 0x47) {
		return containerTS, nil
	}
	if n >= 8 {
		switch string(head[4:8]) {
		case "ftyp", "styp", "moof", "sidx", "emsg", "free":
			return containerFMP4, nil
		}
	}
	return containerUnknown, unsupportedf("%s 既不是 MPEG-TS（缺少 0x47 同步字节）也不是 fMP4（缺少 ISO BMFF 盒）",
		filepath.Base(path))
}

// UnsupportedError 表示这个媒体结构或码点在首期支持范围之外。
// 界面据此明确提示「不支持」，而不是笼统地说下载失败。
type UnsupportedError struct{ Reason string }

func (e *UnsupportedError) Error() string { return e.Reason }

// IsUnsupported 判断一个错误是不是「首期不支持」这一类。
func IsUnsupported(err error) bool {
	var target *UnsupportedError
	return errors.As(err, &target)
}

func unsupportedf(format string, args ...any) error {
	return &UnsupportedError{Reason: fmt.Sprintf(format, args...)}
}
