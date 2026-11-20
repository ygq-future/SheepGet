package media

import (
	"testing"

	"github.com/asticode/go-astits"
)

// TestIsTimedID3Metadata 固定住「只跳过确认是 timed ID3 的元数据轨」这一判断：
// stream_type 与描述符必须同时对上，其它元数据或别的 stream_type 仍应按不支持处理。
func TestIsTimedID3Metadata(t *testing.T) {
	id3Descriptor := func(content []byte) *astits.Descriptor {
		content = append([]byte(nil), content...)
		return &astits.Descriptor{
			Tag:     metadataDescriptorTag,
			Unknown: &astits.DescriptorUnknown{Tag: metadataDescriptorTag, Content: content},
		}
	}
	id3Registration := &astits.Descriptor{
		Tag:          astits.DescriptorTagRegistration,
		Registration: &astits.DescriptorRegistration{FormatIdentifier: id3FormatIdentifierCode},
	}

	cases := []struct {
		name string
		es   *astits.PMTElementaryStream
		want bool
	}{
		{
			name: "metadata descriptor 的 metadata_format_identifier",
			es: &astits.PMTElementaryStream{
				StreamType: astits.StreamTypeMetadata,
				ElementaryStreamDescriptors: []*astits.Descriptor{
					id3Descriptor([]byte{0xFF, 0xFF, 'I', 'D', '3', ' ', 0xFF, 'I', 'D', '3', ' ', 0, 0}),
				},
			},
			want: true,
		},
		{
			name: "metadata descriptor 的 application_format_identifier",
			es: &astits.PMTElementaryStream{
				StreamType: astits.StreamTypeMetadata,
				ElementaryStreamDescriptors: []*astits.Descriptor{
					id3Descriptor([]byte{0xFF, 0xFF, 'I', 'D', '3', ' ', 0x00, 0}),
				},
			},
			want: true,
		},
		{
			name: "registration descriptor",
			es: &astits.PMTElementaryStream{
				StreamType:                  astits.StreamTypeMetadata,
				ElementaryStreamDescriptors: []*astits.Descriptor{id3Registration},
			},
			want: true,
		},
		{
			name: "private data + ID3 registration",
			es: &astits.PMTElementaryStream{
				StreamType:                  astits.StreamTypePrivateData,
				ElementaryStreamDescriptors: []*astits.Descriptor{id3Registration},
			},
			want: true,
		},
		{
			name: "metadata descriptor 不是 ID3",
			es: &astits.PMTElementaryStream{
				StreamType: astits.StreamTypeMetadata,
				ElementaryStreamDescriptors: []*astits.Descriptor{
					id3Descriptor([]byte{0xFF, 0xFF, 'K', 'L', 'V', 'A', 0xFF, 'K', 'L', 'V', 'A', 0, 0}),
				},
			},
			want: false,
		},
		{
			name: "stream_type 不是元数据",
			es: &astits.PMTElementaryStream{
				StreamType: astits.StreamTypeH264Video,
				ElementaryStreamDescriptors: []*astits.Descriptor{
					id3Descriptor([]byte{0xFF, 0xFF, 'I', 'D', '3', ' ', 0xFF, 'I', 'D', '3', ' ', 0, 0}),
				},
			},
			want: false,
		},
		{
			name: "截断的 metadata descriptor",
			es: &astits.PMTElementaryStream{
				StreamType:                  astits.StreamTypeMetadata,
				ElementaryStreamDescriptors: []*astits.Descriptor{id3Descriptor([]byte{0xFF, 0xFF, 'I', 'D'})},
			},
			want: false,
		},
		{
			name: "没有描述符",
			es:   &astits.PMTElementaryStream{StreamType: astits.StreamTypeMetadata},
			want: false,
		},
		{name: "nil", es: nil, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTimedID3Metadata(tc.es); got != tc.want {
				t.Fatalf("isTimedID3Metadata() = %v, want %v", got, tc.want)
			}
		})
	}
}
