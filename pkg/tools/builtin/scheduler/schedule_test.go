package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

func TestParseWhen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		when         string
		wantNext     time.Time
		wantInterval time.Duration
		wantErr      bool
	}{
		{"delay", "in:10m", testNow.Add(10 * time.Minute), 0, false},
		{"at rfc3339", "at:2026-07-13T15:00:00Z", time.Date(2026, 7, 13, 15, 0, 0, 0, time.UTC), 0, false},
		{"every", "every:1h", testNow.Add(time.Hour), time.Hour, false},
		{"minutely", "minutely", testNow.Add(time.Minute), time.Minute, false},
		{"hourly", "hourly", testNow.Add(time.Hour), time.Hour, false},
		{"daily", "daily", testNow.Add(24 * time.Hour), 24 * time.Hour, false},
		{"weekly", "weekly", testNow.Add(7 * 24 * time.Hour), 7 * 24 * time.Hour, false},
		{"whitespace and case", "  Hourly  ", testNow.Add(time.Hour), time.Hour, false},
		{"empty", "", time.Time{}, 0, true},
		{"garbage", "sometimes", time.Time{}, 0, true},
		{"at in past", "at:2020-01-01T00:00:00Z", time.Time{}, 0, true},
		{"every zero", "every:0s", time.Time{}, 0, true},
		{"every negative", "every:-5m", time.Time{}, 0, true},
		{"every below floor", "every:100ms", time.Time{}, 0, true},
		{"every just below floor", "every:59s", time.Time{}, 0, true},
		{"every at floor", "every:1m", testNow.Add(time.Minute), time.Minute, false},
		{"short one-shot allowed", "in:1s", testNow.Add(time.Second), 0, false},
		{"in bad duration", "in:abc", time.Time{}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, interval, err := parseWhen(tt.when, testNow)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantNext, next)
			require.Equal(t, tt.wantInterval, interval)
		})
	}
}
