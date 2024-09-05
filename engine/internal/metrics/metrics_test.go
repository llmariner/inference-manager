package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWindowBucketAdd(t *testing.T) {
	w := newWindowBucket(3 * time.Second)
	now := time.Now().Truncate(percision)
	tt := now

	var tests = []struct {
		name         string
		d            time.Duration
		v            float64
		wantBuckets  []float64
		wantIndex    int
		wantNoUpdate bool
	}{
		{
			name:        "first value",
			d:           0 * time.Second,
			v:           1,
			wantBuckets: []float64{1, 0, 0},
			wantIndex:   0,
		},
		{
			name:        "add 1 (within the window)",
			d:           1 * time.Second,
			v:           1,
			wantBuckets: []float64{1, 2, 0},
			wantIndex:   1,
		},
		{
			name:        "add 2 (within the window)",
			d:           2 * time.Second,
			v:           2,
			wantBuckets: []float64{4, 2, 2},
			wantIndex:   0,
		},
		{
			name:        "out of the window",
			d:           5 * time.Second,
			v:           1,
			wantBuckets: []float64{5, 4, 4},
			wantIndex:   0,
		},
		{
			name:        "add minus value",
			d:           2 * time.Second,
			v:           -1,
			wantBuckets: []float64{5, 5, 4},
			wantIndex:   2,
		},
		{
			name:        "add minus value (same bucket)",
			d:           2 * time.Nanosecond,
			v:           -1,
			wantBuckets: []float64{5, 5, 3},
			wantIndex:   2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tt = tt.Add(test.d)
			w.add(tt, test.v)
			assert.Equal(t, test.wantBuckets, w.buckets)
			assert.Equal(t, test.wantIndex, w.lastIndex)
			assert.Equal(t, tt.Truncate(percision), w.lastUpdatedAt)
		})
	}
}

func TestWindowBucketAverage(t *testing.T) {
	const window = 5 * time.Second
	now := time.Now().Truncate(percision)

	var tests = []struct {
		name    string
		t       time.Time
		w       windowBucket
		wantAvg float64
	}{
		{
			name: "no data",
			t:    now,
		},
		{
			name: "last update is outside the window",
			t:    now.Add(8 * time.Second),
			w: windowBucket{
				buckets:       []float64{1, 4, 4, 3, 2},
				window:        window,
				lastIndex:     3,
				lastUpdatedAt: now.Add(3 * time.Second),
			},
			wantAvg: 3,
		},
		{
			name: "all data is inside the window",
			t:    now.Add(6 * time.Second),
			w: windowBucket{
				buckets:       []float64{1, 2, 3, 4, 5},
				window:        window,
				lastIndex:     4,
				lastUpdatedAt: now.Add(5 * time.Second),
			},
			wantAvg: (1 + 2 + 3 + 4 + 5) / 5,
		},
		{
			name: "last update is inside the window",
			t:    now.Add(6 * time.Second),
			w: windowBucket{
				buckets:       []float64{1, 2, 3, 4, 5},
				window:        window,
				lastIndex:     1,
				lastUpdatedAt: now.Add(3 * time.Second),
			},
			wantAvg: (2*3 + (1 + 5)) / float64(5),
		},
		{
			name: "last update is inside the window (wrap around with mod)",
			t:    now.Add(7 * time.Second),
			w: windowBucket{
				buckets:       []float64{1, 2, 3, 4, 5},
				window:        window,
				lastIndex:     3,
				lastUpdatedAt: now.Add(4 * time.Second),
			},
			wantAvg: (4*3 + (2 + 3)) / float64(5),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.w.average(test.t)
			assert.Equal(t, test.wantAvg, got)
		})
	}
}
