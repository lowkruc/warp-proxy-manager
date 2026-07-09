package docker

import (
	"testing"
)

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"kilobytes", "150k", 153600, false},
		{"kilobytes KB", "150KB", 153600, false},
		{"megabytes", "150m", 157286400, false},
		{"megabytes MB", "150MB", 157286400, false},
		{"gigabytes", "2g", 2147483648, false},
		{"gigabytes GB", "2GB", 2147483648, false},
		{"decimal megabytes", "1.5m", 1572864, false},
		{"raw bytes", "1024", 1024, false},
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"case insensitive", "150M", 157286400, false},
		{"whitespace", "  150m  ", 157286400, false},
		{"unknown unit", "150x", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"long", "12345678901234567890", "123456789012"},
		{"exact", "123456789012", "123456789012"},
		{"short", "abc", "abc"},
		{"empty", "", ""},
		{"one", "a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.id)
			if got != tt.want {
				t.Errorf("truncateID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
