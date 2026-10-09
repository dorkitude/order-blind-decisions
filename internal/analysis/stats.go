// Package analysis computes the preregistered endpoints from the analysis
// database (see plans/v1-design.md, "Confirmatory analysis").
package analysis

import (
	"math/rand/v2"
	"sort"
)

// BootstrapSeed and Resamples are fixed by the frozen analysis plan.
const (
	BootstrapSeed = 20261009
	Resamples     = 10000
)

// agg holds one item's sums for the two groups being compared.
type agg struct{ sA, nA, sB, nB float64 }

// Groups maps item key to its aggregate.
type Groups map[string]*agg

// Add records one observation in group A or B.
func (g Groups) Add(item string, inA bool, v float64) {
	a := g[item]
	if a == nil {
		a = &agg{}
		g[item] = a
	}
	if inA {
		a.sA += v
		a.nA++
	} else {
		a.sB += v
		a.nB++
	}
}

func est(aggs []*agg) float64 {
	var sA, nA, sB, nB float64
	for _, a := range aggs {
		sA, nA, sB, nB = sA+a.sA, nA+a.nA, sB+a.sB, nB+a.nB
	}
	if nA == 0 || nB == 0 {
		return 0
	}
	return sA/nA - sB/nB
}

// Estimate is a point estimate with a 90% percentile interval.
type Estimate struct {
	Value, Lo, Hi float64
	Items         int
}

func percentile(xs []float64, p float64) float64 {
	sort.Float64s(xs)
	i := int(p * float64(len(xs)-1))
	return xs[i]
}

// Diff estimates mean(A) − mean(B), pooled over items, with a cluster
// bootstrap over items.
func Diff(g Groups) Estimate {
	e, _, _ := Paired(g, nil)
	return e
}

// Paired estimates the difference for g, for h, and for g − h using the same
// item resamples. Only items present in both are used when h is given.
func Paired(g, h Groups) (Estimate, Estimate, Estimate) {
	var keys []string
	for k, a := range g {
		if h != nil {
			if b, ok := h[k]; !ok || b.nA+b.nB == 0 {
				continue
			}
		}
		if a.nA+a.nB > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	n := len(keys)
	ga, ha := make([]*agg, n), make([]*agg, n)
	for i, k := range keys {
		ga[i] = g[k]
		if h != nil {
			ha[i] = h[k]
		}
	}
	r := rand.New(rand.NewPCG(BootstrapSeed, BootstrapSeed))
	bg, bh, bd := make([]float64, Resamples), make([]float64, Resamples), make([]float64, Resamples)
	sg, sh := make([]*agg, n), make([]*agg, n)
	for b := 0; b < Resamples; b++ {
		for i := 0; i < n; i++ {
			j := r.IntN(n)
			sg[i] = ga[j]
			if h != nil {
				sh[i] = ha[j]
			}
		}
		bg[b] = est(sg)
		if h != nil {
			bh[b] = est(sh)
			bd[b] = bg[b] - bh[b]
		}
	}
	mk := func(v float64, xs []float64) Estimate {
		return Estimate{Value: v, Lo: percentile(xs, 0.05), Hi: percentile(xs, 0.95), Items: n}
	}
	eg := mk(est(ga), bg)
	if h == nil {
		return eg, Estimate{}, Estimate{}
	}
	eh := mk(est(ha), bh)
	return eg, eh, mk(eg.Value-eh.Value, bd)
}

// Verdict applies the preregistered rule for one endpoint.
func Verdict(e Estimate, margin float64) string {
	switch {
	case e.Lo >= -margin && e.Hi <= margin:
		return "within margin"
	case e.Lo > margin || e.Hi < -margin:
		return "outside margin"
	}
	return "inconclusive"
}
