package main

import (
	"testing"

	"github.com/acxxcxx/armax-coin/core"
)

func TestClockOffsetFromTemplate(t *testing.T) {
	cases := []struct {
		name        string
		local, node int64
		wantOffset  int64
		wantWarn    bool
	}{
		{"in sync", 1000, 1000, 0, false},
		{"small skew ahead", 1050, 1000, 0, false},
		{"exactly at threshold", 1060, 1000, 0, false},
		{"ahead 399s is corrected", 1399, 1000, -399, true},
		{"small skew behind", 950, 1000, 0, false},
		{"far behind is not shifted", 400, 1000, 0, true},
	}
	for _, c := range cases {
		off, warn := clockOffsetFromTemplate(c.local, c.node)
		if off != c.wantOffset || (warn != "") != c.wantWarn {
			t.Errorf("%s: got offset=%d warn=%q, want offset=%d warn=%v", c.name, off, warn, c.wantOffset, c.wantWarn)
		}
		if c.local+off > c.node+ClockSkewWarnSeconds {
			t.Errorf("%s: corrected time still ahead of node by more than the threshold", c.name)
		}
	}
}

func TestFutureLimitWait(t *testing.T) {
	core.SetFutureTimestampLimitActivationHeight(100)
	defer core.ResetFutureTimestampLimitActivationHeightOverride()
	const now = 1_000_000
	limit := int64(core.FutureTimestampLimitSeconds)
	cases := []struct {
		name   string
		height int
		ts     int64
		want   int64
	}{
		{"normal block", 100, now + 5, 0},
		{"inside limit minus margin", 100, now + limit - futureLimitSafetyMargin, 0},
		{"10s past margin waits 10s", 100, now + limit - futureLimitSafetyMargin + 10, 10},
		{"wait is capped at 30s", 100, now + limit + 3600, 30},
		{"below fork the 2h rule applies", 99, now + 3600, 0},
	}
	for _, c := range cases {
		if got := futureLimitWait(c.height, c.ts, now); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
