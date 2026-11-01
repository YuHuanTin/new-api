package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeCaptureRetentionDays(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to default", 0, DefaultCaptureRetentionDays},
		{"minimum", 1, 1},
		{"default", DefaultCaptureRetentionDays, DefaultCaptureRetentionDays},
		{"maximum", MaxCaptureRetentionDays, MaxCaptureRetentionDays},
		{"above maximum is clamped", MaxCaptureRetentionDays + 1, MaxCaptureRetentionDays},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, NormalizeCaptureRetentionDays(tt.in), tt.name)
	}
}

func TestValidateCaptureRetentionDays(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{"default", "3", true},
		{"minimum", "1", true},
		{"maximum", "3650", true},
		{"zero", "0", false},
		{"not a number", "abc", false},
		{"above maximum", "3651", false},
	}
	for _, tt := range tests {
		err := ValidateCaptureRetentionDays(tt.value)
		if tt.ok {
			assert.NoError(t, err, tt.name)
		} else {
			assert.Error(t, err, tt.name)
		}
	}
}
