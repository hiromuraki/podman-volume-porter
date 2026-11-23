package core

import "testing"

func TestFormatSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{size: 0, want: "0 B"},
		{size: 1023, want: "1023 B"},
		{size: 1024, want: "1.0 KiB"},
		{size: 1536, want: "1.5 KiB"},
		{size: 1024 * 1024, want: "1.0 MiB"},
		{size: 3 * 1024 * 1024 * 1024, want: "3.0 GiB"},
		{size: 1024 * 1024 * 1024 * 1024, want: "1.0 TiB"},
	}

	for _, tt := range tests {
		if got := formatSize(tt.size); got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}
