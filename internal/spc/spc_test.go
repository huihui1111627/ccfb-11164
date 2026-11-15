package spc

import (
	"math"
	"testing"
)

func zp(zs ...float64) []ZPoint {
	out := make([]ZPoint, len(zs))
	for i, z := range zs {
		side := 0
		if z > 0 {
			side = 1
		} else if z < 0 {
			side = -1
		}
		out[i] = ZPoint{Index: i + 1, Z: z, Side: side}
	}
	return out
}

func allOn() [8]bool { return [8]bool{true, true, true, true, true, true, true, true} }

func hasRule(hits []RuleHit, rule int) bool {
	for _, h := range hits {
		if h.Rule == rule {
			return true
		}
	}
	return false
}

func TestRule1(t *testing.T) {
	hits := EvalRules(zp(0.1, -0.2, 3.2), allOn())
	if !hasRule(hits, 1) {
		t.Fatal("rule1 not triggered for z=3.2")
	}
	hits = EvalRules(zp(0.1, 2.9), allOn())
	if hasRule(hits, 1) {
		t.Fatal("rule1 false positive")
	}
}

func TestRule2(t *testing.T) {
	hits := EvalRules(zp(0.1, 0.2, 0.3, 0.1, 0.2, 0.1, 0.3, 0.2, 0.1), allOn())
	if !hasRule(hits, 2) {
		t.Fatal("rule2 not triggered for 9 points above CL")
	}
	hits = EvalRules(zp(0.1, 0.2, 0.3, 0.1, 0.2, 0.1, 0.3, 0.2, -0.1), allOn())
	if hasRule(hits, 2) {
		t.Fatal("rule2 false positive")
	}
}

func TestRule3(t *testing.T) {
	hits := EvalRules(zp(0.1, 0.2, 0.3, 0.4, 0.5, 0.6), allOn())
	if !hasRule(hits, 3) {
		t.Fatal("rule3 not triggered for 6 increasing")
	}
}

func TestRule4(t *testing.T) {
	zs := make([]float64, 14)
	for i := range zs {
		if i%2 == 0 {
			zs[i] = 0.5
		} else {
			zs[i] = -0.5
		}
	}
	hits := EvalRules(zp(zs...), allOn())
	if !hasRule(hits, 4) {
		t.Fatal("rule4 not triggered for 14 alternating")
	}
}

func TestRule5(t *testing.T) {
	hits := EvalRules(zp(0.1, 2.3, 2.5), allOn())
	if !hasRule(hits, 5) {
		t.Fatal("rule5 not triggered")
	}
	hits = EvalRules(zp(0.1, 2.3, -2.5), allOn())
	if hasRule(hits, 5) {
		t.Fatal("rule5 false positive: opposite sides")
	}
}

func TestRule6(t *testing.T) {
	hits := EvalRules(zp(1.2, 1.3, 0.2, 1.4, 1.1), allOn())
	if !hasRule(hits, 6) {
		t.Fatal("rule6 not triggered")
	}
}

func TestRule7(t *testing.T) {
	zs := make([]float64, 15)
	for i := range zs {
		zs[i] = 0.3
	}
	hits := EvalRules(zp(zs...), allOn())
	if !hasRule(hits, 7) {
		t.Fatal("rule7 not triggered for 15 within 1σ")
	}
}

func TestRule8(t *testing.T) {
	zs := []float64{1.5, -1.5, 1.6, -1.4, 1.3, -1.7, 1.2, -1.2}
	hits := EvalRules(zp(zs...), allOn())
	if !hasRule(hits, 8) {
		t.Fatal("rule8 not triggered")
	}
}

func TestRuleSwitch(t *testing.T) {
	off := [8]bool{}
	hits := EvalRules(zp(5.0), off)
	if len(hits) != 0 {
		t.Fatal("rules should be all disabled")
	}
}

func TestXbarRLimits(t *testing.T) {
	// n=5, R̄=2 → UCL = X̿ + 0.577*2
	means := []float64{10, 10, 10, 10, 10}
	ranges := []float64{2, 2, 2, 2, 2}
	l := XbarRLimits(means, ranges, 5, "test")
	if math.Abs(l.UCL-(10+0.577*2)) > 1e-9 {
		t.Fatalf("UCL wrong: %v", l.UCL)
	}
	if math.Abs(l.Sigma-2/2.326) > 1e-9 {
		t.Fatalf("sigma wrong: %v", l.Sigma)
	}
}

func TestIMRLimits(t *testing.T) {
	vals := []float64{10, 10.2, 9.8, 10.1, 9.9}
	l := IMRLimits(vals, "test")
	// MR̄ = (0.2+0.4+0.3+0.2)/4 = 0.275
	if math.Abs(l.DCL-0.275) > 1e-9 {
		t.Fatalf("MR bar wrong: %v", l.DCL)
	}
	if math.Abs(l.UCL-(10+2.66*0.275)) > 1e-9 {
		t.Fatalf("UCL wrong: %v", l.UCL)
	}
}

func TestPLimits(t *testing.T) {
	l := PLimits(20, 2000, 200, "test") // p̄=0.01
	if math.Abs(l.CL-0.01) > 1e-12 {
		t.Fatalf("CL wrong: %v", l.CL)
	}
	want := 0.01 + 3*math.Sqrt(0.01*0.99/200)
	if math.Abs(l.UCL-want) > 1e-12 {
		t.Fatalf("UCL wrong: %v want %v", l.UCL, want)
	}
}

func TestCapability(t *testing.T) {
	vals := []float64{9.9, 10.0, 10.1, 10.0, 9.95, 10.05}
	c := CapabilityIndices(vals, 10.6, 9.4, 0.2)
	// Cp = 1.2/(6*0.2) = 1.0
	if math.Abs(c.Cp-1.0) > 1e-9 {
		t.Fatalf("Cp wrong: %v", c.Cp)
	}
	if c.N != 6 || c.OutOfSpecN != 0 {
		t.Fatalf("counts wrong: %+v", c)
	}
}

func TestGageRR(t *testing.T) {
	// 构造: 零件差异大, 重复性小 → %GRR 应很小
	data := make([][][]float64, 3)
	for o := 0; o < 3; o++ {
		data[o] = make([][]float64, 10)
		for p := 0; p < 10; p++ {
			base := float64(p) * 1.0
			data[o][p] = []float64{base + 0.001, base, base - 0.001}
		}
	}
	r := GageRR(GageRRInput{Data: data})
	if r.PctGRR >= 10 {
		t.Fatalf("expected acceptable gage, got %%GRR=%.2f", r.PctGRR)
	}
	if r.NDC < 5 {
		t.Fatalf("expected ndc>=5, got %d", r.NDC)
	}
}
