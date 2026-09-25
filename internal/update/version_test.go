package update

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1      string
		v2      string
		want    int
		wantErr bool
	}{
		{"1.0.0", "1.0.0", 0, false},
		{"v1.0.0", "1.0.0", 0, false},
		{"1.0.1", "1.0.0", 1, false},
		{"1.0.0", "1.0.1", -1, false},
		{"1.1.0", "1.0.9", 1, false},
		{"2.0.0", "1.99.99", 1, false},
		{"v1.2.3", "v1.2.4", -1, false},
		{"v1.2.3-beta", "1.2.3", 0, false},
		{"invalid", "1.0.0", 0, true},
		{"1.0", "1.0.0", 0, true},
	}

	for _, tt := range tests {
		got, err := CompareVersions(tt.v1, tt.v2)
		if (err != nil) != tt.wantErr {
			t.Errorf("CompareVersions(%q, %q) error = %v, wantErr %v", tt.v1, tt.v2, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %v, want %v", tt.v1, tt.v2, got, tt.want)
		}
	}
}
