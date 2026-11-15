package spc

import (
	"math"

	"spc/internal/model"
)

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// StdDev 样本标准差
func StdDev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	s := 0.0
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)-1))
}

// XbarRLimits 均值-极差图控制限: X̿±A2·R̄, R图 D3·R̄ / D4·R̄
func XbarRLimits(means, ranges []float64, n int, reason string) model.Limits {
	xb, rb := mean(means), mean(ranges)
	nn := clampN(n)
	return model.Limits{
		Active:  true,
		CL:      xb,
		UCL:     xb + a2[nn]*rb,
		LCL:     xb - a2[nn]*rb,
		DCL:     rb,
		DUCL:    d4[nn] * rb,
		DLCL:    d3[nn] * rb,
		Sigma:   rb / d2[nn],
		Samples: len(means),
		Reason:  reason,
	}
}

// XbarSLimits 均值-标准差图控制限: X̿±A3·S̄, S图 B3·S̄ / B4·S̄
func XbarSLimits(means, stds []float64, n int, reason string) model.Limits {
	xb, sb := mean(means), mean(stds)
	nn := clampN(n)
	return model.Limits{
		Active:  true,
		CL:      xb,
		UCL:     xb + a3[nn]*sb,
		LCL:     xb - a3[nn]*sb,
		DCL:     sb,
		DUCL:    b4[nn] * sb,
		DLCL:    b3[nn] * sb,
		Sigma:   sb / c4[nn],
		Samples: len(means),
		Reason:  reason,
	}
}

// IMRLimits 单值-移动极差图: X̄±2.66·MR̄, MR图 UCL=3.267·MR̄
func IMRLimits(vals []float64, reason string) model.Limits {
	xb := mean(vals)
	mrs := make([]float64, 0, len(vals)-1)
	for i := 1; i < len(vals); i++ {
		mrs = append(mrs, math.Abs(vals[i]-vals[i-1]))
	}
	mrb := mean(mrs)
	return model.Limits{
		Active:  true,
		CL:      xb,
		UCL:     xb + 2.66*mrb,
		LCL:     xb - 2.66*mrb,
		DCL:     mrb,
		DUCL:    3.267 * mrb,
		DLCL:    0,
		Sigma:   mrb / 1.128,
		Samples: len(vals),
		Reason:  reason,
	}
}

// PLimits 不良率图控制限: p̄±3√(p̄(1-p̄)/n̄)
func PLimits(totalDef, totalInsp int, avgN float64, reason string) model.Limits {
	p := float64(totalDef) / float64(totalInsp)
	s := math.Sqrt(p * (1 - p) / avgN)
	lcl := p - 3*s
	if lcl < 0 {
		lcl = 0
	}
	return model.Limits{
		Active:  true,
		CL:      p,
		UCL:     p + 3*s,
		LCL:     lcl,
		Sigma:   s,
		Samples: totalInsp,
		Reason:  reason,
	}
}
