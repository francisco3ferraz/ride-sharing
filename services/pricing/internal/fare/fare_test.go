package fare

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEstimate(t *testing.T) {
	tests := []struct {
		name           string
		distanceMeters int32
		duration       time.Duration
		want           int64
	}{
		{
			name:           "typical trip",
			distanceMeters: 5_000,
			duration:       10 * time.Minute,
			want:           750, // 150 + 400 + 200
		},
		{
			name:           "short trip pays the minimum",
			distanceMeters: 500,
			duration:       time.Minute,
			want:           400, // 150 + 40 + 20 = 210, raised to 400
		},
		{
			name:           "fractions are added before rounding",
			distanceMeters: 12_345,
			duration:       17*time.Minute + 30*time.Second,
			want:           1488, // 150 + 987.6 + 350 = 1487.6
		},
		{
			name:           "half a cent rounds up",
			distanceMeters: 10_000,
			duration:       1500 * time.Millisecond,
			want:           951, // 150 + 800 + 0.5
		},
		{
			name:           "less than half a cent rounds down",
			distanceMeters: 10_000,
			duration:       time.Second,
			want:           950, // 150 + 800 + 0.33
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Estimate(tt.distanceMeters, tt.duration))
		})
	}
}
