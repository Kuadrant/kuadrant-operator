//go:build unit

package v1beta3

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
			name: "seconds",
			rate: Rate{
				Limit: 5, Duration: 2, Unit: TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2,
		},
		{
			name: "minutes",
			rate: Rate{
				Limit: 5, Duration: 2, Unit: TimeUnit("minute"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60,
		},
		{
			name: "hours",
			rate: Rate{
				Limit: 5, Duration: 2, Unit: TimeUnit("hour"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60 * 60,
		},
		{
			name: "day",
			rate: Rate{
				Limit: 5, Duration: 2, Unit: TimeUnit("day"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  2 * 60 * 60 * 24,
		},
		{
			name: "negative limit",
			rate: Rate{
				Limit: -5, Duration: 2, Unit: TimeUnit("second"),
			},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name: "negative duration",
			rate: Rate{
				Limit: 5, Duration: -2, Unit: TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name: "limit  is 0",
			rate: Rate{
				Limit: 0, Duration: 2, Unit: TimeUnit("second"),
			},
			expectedMaxValue: 0,
			expectedSeconds:  2,
		},
		{
			name: "rate is 0",
			rate: Rate{
				Limit: 5, Duration: 0, Unit: TimeUnit("second"),
			},
			expectedMaxValue: 5,
			expectedSeconds:  0,
		},
		{
			name: "unexpected time unit",
			rate: Rate{
				Limit: 5, Duration: 2, Unit: TimeUnit("unknown"),
			},
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
