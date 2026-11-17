package core

import (
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"
)

// Offline C6 study: self-contained model, uses no consensus code and changes no behavior.
// Run: ARMAX_C6_SIM=1 go test ./core -run TestC6TimestampSimulation -v

type c6Miner struct {
	share   float64
	offset  float64 // constant clock offset in seconds
	lagMean float64 // mean template age in seconds (header stamped at template creation)
}

type c6Params struct {
	limit  float64 // future limit in seconds
	window int
	clamp  int64 // solve time clamp, multiples of T
}

type c6Block struct {
	ts   int64
	diff float64
}

type c6Result struct {
	meanSteady, meanStep, sdStep, zeroFrac, rejectFrac, maxDiffOvershoot float64
}

func c6Median(v []int64) int64 {
	c := append([]int64(nil), v...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

func c6NextDiff(chain []c6Block, p c6Params) float64 {
	n := p.window
	if len(chain) < n+1 {
		return chain[len(chain)-1].diff
	}
	w := chain[len(chain)-(n+1):]
	T := float64(TargetBlockTimeSeconds)
	var wst, sumT float64
	for i := 1; i <= n; i++ {
		st := float64(w[i].ts - w[i-1].ts)
		lim := float64(p.clamp) * T
		st = math.Max(-lim, math.Min(lim, st))
		wst += float64(i) * st
		sumT += 1 / w[i].diff
	}
	exp := float64(n*(n+1)/2) * T
	wst = math.Max(exp/4, math.Min(exp*4, wst))
	next := (sumT / float64(n)) * wst / exp
	return 1 / next
}

func c6Run(seed int64, miners []c6Miner, p c6Params, hrBefore, hrAfter float64) c6Result {
	r := rand.New(rand.NewSource(seed))
	const preBlocks, stepAt, total = 200, 1000, 2000
	T := float64(TargetBlockTimeSeconds)
	now := 0.0
	chain := make([]c6Block, 0, total+preBlocks)
	for i := 0; i <= preBlocks; i++ {
		chain = append(chain, c6Block{ts: int64(i) * int64(T), diff: hrBefore})
	}
	now = float64(preBlocks) * T
	var trueT []float64
	var zero, found, rejected int
	var maxDiff float64
	for len(chain)-preBlocks-1 < total {
		h := len(chain) - preBlocks - 1
		hr := hrBefore
		if h >= stepAt {
			hr = hrAfter
		}
		d := c6NextDiff(chain, p)
		if h >= stepAt {
			maxDiff = math.Max(maxDiff, d/hrAfter)
		}
		now += r.ExpFloat64() * T * d / hr
		u, acc := r.Float64(), 0.0
		m := miners[len(miners)-1]
		for _, c := range miners {
			acc += c.share
			if u <= acc {
				m = c
				break
			}
		}
		found++
		ts := now + m.offset
		if m.lagMean > 0 {
			ts -= math.Min(r.ExpFloat64()*m.lagMean, 800)
		}
		tsI := int64(ts)
		par := chain[len(chain)-1].ts
		lastN := len(chain)
		if lastN > 11 {
			lastN = 11
		}
		var recent []int64
		for _, b := range chain[len(chain)-lastN:] {
			recent = append(recent, b.ts)
		}
		if mtp := c6Median(recent) + 1; tsI < mtp {
			tsI = mtp
		}
		if tsI < par {
			tsI = par
		}
		if float64(tsI) > now+p.limit {
			rejected++
			continue
		}
		if h > 0 {
			trueT = append(trueT, now)
		}
		if tsI-par <= 0 {
			zero++
		}
		chain = append(chain, c6Block{ts: tsI, diff: d})
	}
	_ = zero
	var res c6Result
	mean := func(a, b int) float64 {
		s := 0.0
		for i := a + 1; i < b; i++ {
			s += trueT[i] - trueT[i-1]
		}
		return s / float64(b-a-1)
	}
	res.meanSteady = mean(300, stepAt-1)
	res.meanStep = mean(stepAt, stepAt+100)
	var sq float64
	for i := stepAt + 1; i < stepAt+100; i++ {
		x := trueT[i] - trueT[i-1] - res.meanStep
		sq += x * x
	}
	res.sdStep = math.Sqrt(sq / 99)
	res.zeroFrac = float64(zero) / float64(total)
	res.rejectFrac = float64(rejected) / float64(found)
	res.maxDiffOvershoot = maxDiff
	return res
}

func TestC6TimestampSimulation(t *testing.T) {
	if os.Getenv("ARMAX_C6_SIM") == "" {
		t.Skip("set ARMAX_C6_SIM=1 to run the offline C6 study")
	}
	profiles := map[string][]c6Miner{
		"honest": {{share: 1}},
		// Shares and clock behavior observed in the testnet logs (db_review, blocks >= 8000).
		"observed": {
			{share: 0.49, lagMean: 90},
			{share: 0.30},
			{share: 0.12, offset: 399},
			{share: 0.09},
		},
	}
	const seeds = 60
	for _, name := range []string{"honest", "observed"} {
		t.Logf("profile=%s (hashrate x1 -> x3 at block 1000, %d seeds)", name, seeds)
		t.Logf("limit  N  clamp | steady  step100  sdStep  zero%%  rejected%%  maxDiff/H")
		for _, limit := range []float64{7200, 900, 600, 300} {
			for _, n := range []int{60, 45, 30} {
				for _, cl := range []int64{6, 4} {
					p := c6Params{limit: limit, window: n, clamp: cl}
					var a c6Result
					for s := 0; s < seeds; s++ {
						r := c6Run(int64(s+1), profiles[name], p, 1, 3)
						a.meanSteady += r.meanSteady
						a.meanStep += r.meanStep
						a.sdStep += r.sdStep
						a.zeroFrac += r.zeroFrac
						a.rejectFrac += r.rejectFrac
						a.maxDiffOvershoot += r.maxDiffOvershoot
					}
					f := float64(seeds)
					t.Logf("%5.0f %3d %2dT   | %6.1f %7.1f %7.1f %6.1f %9.1f %9.2f",
						limit, n, cl, a.meanSteady/f, a.meanStep/f, a.sdStep/f,
						100*a.zeroFrac/f, 100*a.rejectFrac/f, a.maxDiffOvershoot/f)
				}
			}
		}
	}
}
