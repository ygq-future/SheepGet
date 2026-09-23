package engine

import (
	"testing"
	"time"
)

// 退避策略：指数增长、封顶，且不随尝试次数溢出——被限速的站点越推越远的成本太高，
// 而网络恢复后又必须及时续上。
func TestRetryBackoff_GrowsAndCaps(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{-1, retryBaseDelay},
		{0, retryBaseDelay},
		{1, 600 * time.Millisecond},
		{2, 1200 * time.Millisecond},
		{5, 9600 * time.Millisecond},
		{6, retryMaxDelay},
		{50, retryMaxDelay},
	}
	for _, tc := range tests {
		if got := retryBackoff(tc.attempt); got != tc.want {
			t.Errorf("retryBackoff(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}
