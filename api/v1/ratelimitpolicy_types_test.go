//go:build unit

package v1

import (
	"testing"
)

func TestConvertRateIntoSeconds(t *testing.T) {
	testCases := []struct {
		name             string
		rate             Rate
		expectedMaxValue int
		expectedSeconds  int
	}{
		{
			name:             "seconds",
			rate:             Rate{Limit: 5, Window: Duration("2s")},
			expectedMaxValue: 5,
			expectedSeconds:  2,
		},
		{
			name:             "minutes",
			rate:             Rate{Limit: 5, Window: Duration("2m")},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60,
		},
		{
			name:             "hours",
			rate:             Rate{Limit: 5, Window: Duration("2h")},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60 * 60,
		},
		{
			name:             "negative limit",
			rate:             Rate{Limit: -5, Window: Duration("2s")},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name:             "limit  is 0",
			rate:             Rate{Limit: 0, Window: Duration("2s")},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name:             "rate is 0",
			rate:             Rate{Limit: 5, Window: Duration("0s")},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name:             "invalid duration 01",
			rate:             Rate{Limit: 5, Window: Duration("unknown")},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name:             "invalid duration 02",
			rate:             Rate{Limit: 5, Window: Duration("5d")},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			maxValue, seconds := tc.rate.ToSeconds()
			if maxValue != tc.expectedMaxValue {
				subT.Errorf("maxValue does not match, expected(%d), got (%d)", tc.expectedMaxValue, maxValue)
			}
			if seconds != tc.expectedSeconds {
				subT.Errorf("seconds does not match, expected(%d), got (%d)", tc.expectedSeconds, seconds)
			}
		})
	}
}
