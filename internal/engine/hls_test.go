package engine_test

import (
	"testing"

	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
)

func TestHLSVariantFilename(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		variant  hls.Variant
		expected string
	}{
		{
			name:     "standard 420p",
			base:     "video.mp4",
			variant:  hls.Variant{Height: 420},
			expected: "video_420p.mp4",
		},
		{
			name:     "standard 1080p with m3u8 extension",
			base:     "stream.m3u8",
			variant:  hls.Variant{Height: 1080},
			expected: "stream_1080p.mp4",
		},
		{
			name:     "already contains same resolution tag",
			base:     "movie_720p.mp4",
			variant:  hls.Variant{Height: 720},
			expected: "movie_720p.mp4",
		},
		{
			name:     "swapping resolution tag from 720p to 1080p",
			base:     "movie_720p.mp4",
			variant:  hls.Variant{Height: 1080},
			expected: "movie_1080p.mp4",
		},
		{
			name:     "empty base filename defaults to media",
			base:     "",
			variant:  hls.Variant{Height: 420},
			expected: "media_420p.mp4",
		},
		{
			name:     "variant with name 480p instead of height",
			base:     "sample.mp4",
			variant:  hls.Variant{Name: "480p"},
			expected: "sample_480p.mp4",
		},
		{
			name:     "no resolution info preserves base name",
			base:     "sample.mp4",
			variant:  hls.Variant{},
			expected: "sample.mp4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.HLSVariantFilename(tc.base, tc.variant)
			if got != tc.expected {
				t.Errorf("HLSVariantFilename(%q, %+v) = %q; want %q", tc.base, tc.variant, got, tc.expected)
			}
		})
	}
}
