// Package hls 实现 HLS 点播下载链路：清单解析、分片下载、AES-128 解密与媒体信息组织。
//
// 按 ADR-0001，本包只负责「清单、分片、加密、下载及信息组织」，不做任何媒体解析与封装；
// 分片落盘后交给 internal/media 的 Media Processor 处理。清单解析按 RFC 8216 只实现点播
// 需要的部分：直播语义（滚动窗口、缺少 ENDLIST）不参与行为，遇到时按点播尽力处理。
package hls

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Kind 表示清单类型。
type Kind string

const (
	KindMaster Kind = "master"
	KindMedia  Kind = "media"
)

// MediaEntry 是主清单里的一条 EXT-X-MEDIA，用于把独立音轨与变体关联起来。
type MediaEntry struct {
	Type     string `json:"type"` // AUDIO / VIDEO / SUBTITLES / CLOSED-CAPTIONS
	GroupID  string `json:"groupId"`
	Name     string `json:"name,omitempty"`
	URI      string `json:"uri,omitempty"`
	Language string `json:"language,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Channels string `json:"channels,omitempty"`
}

// Variant 是主清单里的一个清晰度选项，对应一条 EXT-X-STREAM-INF 及其后的媒体清单地址。
type Variant struct {
	URI          string  `json:"uri"`
	Bandwidth    int     `json:"bandwidth"`
	Width        int     `json:"width,omitempty"`
	Height       int     `json:"height,omitempty"`
	Codecs       string  `json:"codecs,omitempty"`
	Name         string  `json:"name,omitempty"`
	FrameRate    float64 `json:"frameRate,omitempty"`
	AudioGroupID string  `json:"audioGroupId,omitempty"`
}

// VariantOption 是给界面看的清晰度选项：只有用户据以做决定、以及回传给后端的那几项。
// 清单地址与码率来自 EXT-X-STREAM-INF；展示名（Label）已把分辨率说清楚（如 1080P），
// 界面据此渲染即可，不需要再读一遍宽高。
type VariantOption struct {
	URI       string `json:"uri"`
	Label     string `json:"label"`
	Bandwidth int    `json:"bandwidth,omitempty"`
}

// VariantOptions 把解析出的清晰度整理成界面选项。
//
// 展示名在这里算一次，桌面端对话框与浏览器扩展拿到的是同一个名字——两处各自命名会让
// 同一个清晰度在不同入口显示成不同的东西。
func VariantOptions(variants []Variant) []VariantOption {
	if len(variants) == 0 {
		return nil
	}
	opts := make([]VariantOption, 0, len(variants))
	for _, v := range variants {
		opts = append(opts, VariantOption{
			URI:       v.URI,
			Label:     v.Label(),
			Bandwidth: v.Bandwidth,
		})
	}
	return opts
}

// Label 按清单给出的属性取名。三样属性都没有时留空——那不是「未知清晰度」，
// 而是这份清单根本没说明它是什么；界面按序号呈现，不编一个看起来像规格的名字。
func (v Variant) Label() string {
	if v.Name != "" {
		return v.Name
	}
	if v.Height > 0 {
		return strconv.Itoa(v.Height) + "P"
	}
	if v.Bandwidth > 0 {
		return fmt.Sprintf("%.1f Mbps", float64(v.Bandwidth)/1e6)
	}
	return ""
}

// Key 是一条 EXT-X-KEY。
type Key struct {
	Method string `json:"method"`
	URI    string `json:"uri,omitempty"`
	// IV 为 16 字节向量；为空表示按分片的媒体序号推导（RFC 8216 §5.2）。
	IV []byte `json:"iv,omitempty"`
}

// InitSection 是一条 EXT-X-MAP：fMP4 分片共用的初始化片段。
type InitSection struct {
	URI         string `json:"uri"`
	RangeStart  int64  `json:"rangeStart,omitempty"`
	RangeLength int64  `json:"rangeLength,omitempty"`
}

// Segment 是媒体清单里的一个分片。
type Segment struct {
	URI         string       `json:"uri"`
	Duration    float64      `json:"duration"`
	MediaSeq    int          `json:"mediaSeq"`
	Key         *Key         `json:"key,omitempty"`
	Map         *InitSection `json:"map,omitempty"`
	RangeStart  int64        `json:"rangeStart,omitempty"`
	RangeLength int64        `json:"rangeLength,omitempty"`
}

// MasterPlaylist 是解析后的主清单。
type MasterPlaylist struct {
	Variants []Variant    `json:"variants"`
	Media    []MediaEntry `json:"media,omitempty"`
}

// MediaPlaylist 是解析后的媒体清单（点播视角：分片一次性给全）。
type MediaPlaylist struct {
	Version        int       `json:"version"`
	TargetDuration int       `json:"targetDuration"`
	MediaSequence  int       `json:"mediaSequence"`
	EndList        bool      `json:"endList"`
	Duration       float64   `json:"duration"`
	Segments       []Segment `json:"segments"`
}

// KnownSize 只在清单自己给出了全部分片长度时（EXT-X-BYTERANGE）返回总大小，否则返回 -1。
// 分片长度通常不在清单里，只能逐个问服务器，因此这里不做任何推测。
func (pl *MediaPlaylist) KnownSize() int64 {
	if pl == nil || len(pl.Segments) == 0 {
		return -1
	}
	var total int64
	for _, seg := range pl.Segments {
		if seg.RangeLength <= 0 {
			return -1
		}
		total += seg.RangeLength
	}
	return total
}

// ParseResult 是按内容嗅探出的清单，两者只有一个非空。
type ParseResult struct {
	Kind   Kind
	Master *MasterPlaylist
	Media  *MediaPlaylist
}

// Parse 按内容判断清单类型并解析。base 是清单自身的地址，用于把相对地址解析成绝对地址。
func Parse(base *url.URL, data []byte) (*ParseResult, error) {
	lines := splitLines(string(data))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(trimBOM(strings.TrimSpace(line)), "#EXTM3U") {
			return nil, fmt.Errorf("不是有效的 HLS 清单：首行不是 #EXTM3U")
		}
		lines = lines[i:]
		break
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			master, err := parseMaster(base, lines)
			return &ParseResult{Kind: KindMaster, Master: master}, err
		case strings.HasPrefix(line, "#EXTINF:"), strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"),
			strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
			media, err := parseMedia(base, lines)
			return &ParseResult{Kind: KindMedia, Media: media}, err
		}
	}

	return nil, fmt.Errorf("无法识别的 HLS 清单：既没有变体（EXT-X-STREAM-INF），也没有媒体分片（EXTINF）")
}

func parseMaster(base *url.URL, lines []string) (*MasterPlaylist, error) {
	out := &MasterPlaylist{}
	var pending *Variant

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			if pending != nil {
				pending.URI = resolveURI(base, line)
				out.Variants = append(out.Variants, *pending)
				pending = nil
			}
			continue
		}

		tag, value, _ := strings.Cut(line, ":")
		switch tag {
		case "#EXT-X-STREAM-INF":
			attrs := parseAttributes(value)
			v := Variant{
				Bandwidth:    atoiAttr(attrs, "BANDWIDTH"),
				Codecs:       attrs["CODECS"],
				Name:         attrs["NAME"],
				AudioGroupID: attrs["AUDIO"],
			}
			if res := attrs["RESOLUTION"]; res != "" {
				w, h, ok := strings.Cut(res, "x")
				if ok {
					v.Width, _ = strconv.Atoi(strings.TrimSpace(w))
					v.Height, _ = strconv.Atoi(strings.TrimSpace(h))
				}
			}
			if fr := attrs["FRAME-RATE"]; fr != "" {
				v.FrameRate, _ = strconv.ParseFloat(fr, 64)
			}
			pending = &v

		case "#EXT-X-MEDIA":
			attrs := parseAttributes(value)
			entry := MediaEntry{
				Type:     strings.ToUpper(attrs["TYPE"]),
				GroupID:  attrs["GROUP-ID"],
				Name:     attrs["NAME"],
				Language: attrs["LANGUAGE"],
				Default:  strings.EqualFold(attrs["DEFAULT"], "YES"),
				Channels: attrs["CHANNELS"],
			}
			if uri := attrs["URI"]; uri != "" {
				entry.URI = resolveURI(base, uri)
			}
			out.Media = append(out.Media, entry)
		}
	}

	if len(out.Variants) == 0 {
		return nil, fmt.Errorf("主清单里没有可下载的清晰度（EXT-X-STREAM-INF）")
	}
	return out, nil
}

func parseMedia(base *url.URL, lines []string) (*MediaPlaylist, error) {
	out := &MediaPlaylist{}
	var curKey *Key
	var curMap *InitSection
	var pendingDuration float64
	var pendingRangeStart, pendingRangeLength int64
	var lastRangeEnd int64
	seq := 0

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "#") {
			out.Segments = append(out.Segments, Segment{
				URI:         resolveURI(base, line),
				Duration:    pendingDuration,
				MediaSeq:    out.MediaSequence + seq,
				Key:         curKey,
				Map:         curMap,
				RangeStart:  pendingRangeStart,
				RangeLength: pendingRangeLength,
			})
			seq++
			pendingDuration = 0
			pendingRangeStart, pendingRangeLength = 0, 0
			continue
		}

		tag, value, _ := strings.Cut(line, ":")
		switch tag {
		case "#EXT-X-VERSION":
			out.Version, _ = strconv.Atoi(strings.TrimSpace(value))

		case "#EXT-X-TARGETDURATION":
			out.TargetDuration, _ = strconv.Atoi(strings.TrimSpace(value))

		case "#EXT-X-MEDIA-SEQUENCE":
			out.MediaSequence, _ = strconv.Atoi(strings.TrimSpace(value))

		case "#EXT-X-ENDLIST":
			out.EndList = true

		case "#EXTINF":
			head, _, _ := strings.Cut(value, ",")
			pendingDuration, _ = strconv.ParseFloat(strings.TrimSpace(head), 64)

		case "#EXT-X-KEY":
			attrs := parseAttributes(value)
			method := attrs["METHOD"]
			if method == "" || strings.EqualFold(method, "NONE") {
				curKey = nil
				continue
			}
			key := &Key{Method: method}
			if uri := attrs["URI"]; uri != "" {
				key.URI = resolveURI(base, uri)
			}
			if iv := attrs["IV"]; iv != "" {
				decoded, err := decodeIV(iv)
				if err != nil {
					return nil, err
				}
				key.IV = decoded
			}
			curKey = key

		case "#EXT-X-MAP":
			attrs := parseAttributes(value)
			init := &InitSection{URI: resolveURI(base, attrs["URI"])}
			if br := attrs["BYTERANGE"]; br != "" {
				start, length, err := parseByteRange(br, 0)
				if err != nil {
					return nil, err
				}
				init.RangeStart, init.RangeLength = start, length
			}
			curMap = init

		case "#EXT-X-BYTERANGE":
			start, length, err := parseByteRange(strings.TrimSpace(value), lastRangeEnd)
			if err != nil {
				return nil, err
			}
			pendingRangeStart, pendingRangeLength = start, length
			lastRangeEnd = start + length
		}
	}

	if len(out.Segments) == 0 {
		return nil, fmt.Errorf("媒体清单里没有分片（EXTINF）")
	}
	for i := range out.Segments {
		out.Duration += out.Segments[i].Duration
	}
	return out, nil
}

func parseByteRange(value string, fallbackStart int64) (int64, int64, error) {
	lengthPart, offsetPart, hasOffset := strings.Cut(value, "@")
	length, err := strconv.ParseInt(strings.TrimSpace(lengthPart), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("无效的 BYTERANGE: %q", value)
	}
	start := fallbackStart
	if hasOffset {
		start, err = strconv.ParseInt(strings.TrimSpace(offsetPart), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("无效的 BYTERANGE 偏移: %q", value)
		}
	}
	return start, length, nil
}

func decodeIV(raw string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "0x"), "0X")
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("无效的 EXT-X-KEY IV: %q", raw)
	}
	if len(decoded) != 16 {
		return nil, fmt.Errorf("EXT-X-KEY IV 必须是 16 字节，实际 %d 字节", len(decoded))
	}
	return decoded, nil
}

// resolveURI 把清单中的相对地址解析为绝对地址；已经是绝对地址时原样返回。
func resolveURI(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || base == nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

// parseAttributes 解析 HLS 标签里的 KEY=VALUE 列表，等号右侧的引号内允许出现逗号。
func parseAttributes(s string) map[string]string {
	out := make(map[string]string)
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ',' || s[i] == ' ') {
			i++
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != ',' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			break
		}
		key := strings.ToUpper(strings.TrimSpace(s[start:i]))
		i++ // '='
		var value strings.Builder
		if i < len(s) && s[i] == '"' {
			i++
			for i < len(s) && s[i] != '"' {
				value.WriteByte(s[i])
				i++
			}
			i++ // 收尾引号
		} else {
			for i < len(s) && s[i] != ',' {
				value.WriteByte(s[i])
				i++
			}
		}
		if key != "" {
			out[key] = strings.TrimSpace(value.String())
		}
	}
	return out
}

func atoiAttr(attrs map[string]string, key string) int {
	n, _ := strconv.Atoi(attrs[key])
	return n
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func trimBOM(s string) string {
	return strings.TrimPrefix(s, "\ufeff")
}
