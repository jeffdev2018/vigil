package service

import (
	"testing"
	"time"
)

// Off-peak batch lane (K45). The window decision is the whole feature — the
// scheduler asks InBatchWindow once and stamps the lane on the answer — so the
// matrix lives here and the DB-backed tests only check that the path calls it.

func TestBatchWindowFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     BatchWindow
	}{
		{
			name:     "empty settings are the disabled window",
			settings: "",
			want:     BatchWindow{Timezone: "UTC"},
		},
		{
			name:     "settings without the key are the disabled window",
			settings: `{"code_health":{"enabled":true}}`,
			want:     BatchWindow{Timezone: "UTC"},
		},
		{
			name:     "unparseable blob is the disabled window",
			settings: `{not json`,
			want:     BatchWindow{Timezone: "UTC"},
		},
		{
			name:     "a stored window round-trips",
			settings: `{"batch_window":{"enabled":true,"start_local_time":"22:00","end_local_time":"06:00","timezone":"Europe/Paris"}}`,
			want:     BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "Europe/Paris"},
		},
		{
			name:     "a missing timezone defaults to UTC",
			settings: `{"batch_window":{"enabled":true,"start_local_time":"01:00","end_local_time":"05:00"}}`,
			want:     BatchWindow{Enabled: true, Start: "01:00", End: "05:00", Timezone: "UTC"},
		},
		{
			name:     "a hand-edited invalid window reads as disabled, not as all-day",
			settings: `{"batch_window":{"enabled":true,"start_local_time":"25:00","end_local_time":"06:00","timezone":"UTC"}}`,
			want:     BatchWindow{Timezone: "UTC"},
		},
		{
			name:     "a window whose timezone no longer exists reads as disabled",
			settings: `{"batch_window":{"enabled":true,"start_local_time":"22:00","end_local_time":"06:00","timezone":"Mars/Olympus"}}`,
			want:     BatchWindow{Timezone: "UTC"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BatchWindowFromSettings([]byte(tt.settings))
			if got != tt.want {
				t.Errorf("BatchWindowFromSettings(%s) = %+v, want %+v", tt.settings, got, tt.want)
			}
		})
	}
}

func TestValidateBatchWindow(t *testing.T) {
	tests := []struct {
		name    string
		window  BatchWindow
		wantErr bool
	}{
		{
			name:   "a disabled window is storable whatever its times",
			window: BatchWindow{Enabled: false, Start: "", End: "", Timezone: "UTC"},
		},
		{
			name:   "a same-day window is valid",
			window: BatchWindow{Enabled: true, Start: "01:00", End: "05:00", Timezone: "UTC"},
		},
		{
			name:   "a window crossing midnight is valid",
			window: BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "Europe/Paris"},
		},
		{
			name:    "an unknown timezone is rejected",
			window:  BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "Mars/Olympus"},
			wantErr: true,
		},
		{
			name:    "an unknown timezone is rejected even while disabled",
			window:  BatchWindow{Enabled: false, Timezone: "Mars/Olympus"},
			wantErr: true,
		},
		{
			name:    "equal bounds are ambiguous and rejected",
			window:  BatchWindow{Enabled: true, Start: "03:00", End: "03:00", Timezone: "UTC"},
			wantErr: true,
		},
		{
			name:    "an enabled window needs a start time",
			window:  BatchWindow{Enabled: true, Start: "", End: "06:00", Timezone: "UTC"},
			wantErr: true,
		},
		{
			name:    "an out-of-range hour is rejected",
			window:  BatchWindow{Enabled: true, Start: "24:00", End: "06:00", Timezone: "UTC"},
			wantErr: true,
		},
		{
			name:    "an out-of-range minute is rejected",
			window:  BatchWindow{Enabled: true, Start: "22:60", End: "06:00", Timezone: "UTC"},
			wantErr: true,
		},
		{
			name:    "a single-digit hour is rejected: the stored form is what the UI renders back",
			window:  BatchWindow{Enabled: true, Start: "2:00", End: "06:00", Timezone: "UTC"},
			wantErr: true,
		},
		{
			name:    "seconds are rejected",
			window:  BatchWindow{Enabled: true, Start: "22:00:00", End: "06:00", Timezone: "UTC"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBatchWindow(NormalizeBatchWindow(tt.window))
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBatchWindow(%+v) error = %v, wantErr %v", tt.window, err, tt.wantErr)
			}
		})
	}
}

func TestInBatchWindow(t *testing.T) {
	sameDay := BatchWindow{Enabled: true, Start: "01:00", End: "05:00", Timezone: "UTC"}
	crossMidnight := BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "UTC"}
	paris := BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "Europe/Paris"}

	at := func(hour, minute int) time.Time {
		return time.Date(2026, 3, 10, hour, minute, 0, 0, time.UTC)
	}

	tests := []struct {
		name   string
		window BatchWindow
		now    time.Time
		want   bool
	}{
		{name: "same-day, inside", window: sameDay, now: at(3, 0), want: true},
		{name: "same-day, before", window: sameDay, now: at(0, 59), want: false},
		{name: "same-day, after", window: sameDay, now: at(5, 1), want: false},
		{name: "same-day, start is inclusive", window: sameDay, now: at(1, 0), want: true},
		{name: "same-day, end is exclusive", window: sameDay, now: at(5, 0), want: false},

		{name: "cross-midnight, late evening", window: crossMidnight, now: at(23, 30), want: true},
		{name: "cross-midnight, small hours", window: crossMidnight, now: at(2, 0), want: true},
		{name: "cross-midnight, midday is outside", window: crossMidnight, now: at(12, 0), want: false},
		{name: "cross-midnight, start is inclusive", window: crossMidnight, now: at(22, 0), want: true},
		{name: "cross-midnight, end is exclusive", window: crossMidnight, now: at(6, 0), want: false},
		{name: "cross-midnight, one minute before start", window: crossMidnight, now: at(21, 59), want: false},

		// 23:30 UTC is 00:30 in Paris (CET, UTC+1) — inside the local window.
		{name: "the window is read in its own timezone, not UTC", window: paris, now: at(23, 30), want: true},
		// 21:30 UTC is 22:30 in Paris: outside the UTC reading, inside the local one.
		{name: "a UTC-outside instant can be locally inside", window: paris, now: at(21, 30), want: true},
		// 06:30 UTC is 07:30 in Paris: past the local end.
		{name: "a UTC-inside instant can be locally outside", window: paris, now: at(5, 30), want: false},

		{name: "a disabled window is never inside", window: BatchWindow{Start: "00:00", End: "23:59", Timezone: "UTC"}, now: at(12, 0), want: false},
		{name: "an invalid window is never inside", window: BatchWindow{Enabled: true, Start: "nope", End: "06:00", Timezone: "UTC"}, now: at(2, 0), want: false},
		{name: "an unknown timezone is never inside", window: BatchWindow{Enabled: true, Start: "22:00", End: "06:00", Timezone: "Mars/Olympus"}, now: at(23, 0), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InBatchWindow(tt.window, tt.now); got != tt.want {
				t.Errorf("InBatchWindow(%+v, %s) = %v, want %v", tt.window, tt.now.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}
