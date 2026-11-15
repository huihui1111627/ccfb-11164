package spc

import "math"

// MSA 量具重复性与再现性(Gage R&R, 均值极差法, AIAG MSA-4)

// GageRRInput data[op][part][trial]
type GageRRInput struct {
	Data       [][][]float64
	Operators  []string
	PartLabels []string
}

type GageRRResult struct {
	Parts      int     `json:"parts"`
	Operators  int     `json:"operators"`
	Trials     int     `json:"trials"`
	EV         float64 `json:"ev"`  // 重复性(设备变差)
	AV         float64 `json:"av"`  // 再现性(评价人变差)
	GRR        float64 `json:"grr"` // 量具R&R
	PV         float64 `json:"pv"`  // 零件变差
	TV         float64 `json:"tv"`  // 总变差
	PctEV      float64 `json:"pctEV"`
	PctAV      float64 `json:"pctAV"`
	PctGRR     float64 `json:"pctGRR"` // %GRR, <10%可接受, 10-30%有条件接受
	PctPV      float64 `json:"pctPV"`
	NDC        int     `json:"ndc"` // 可区分类别数, >=5 可接受
	Acceptable string  `json:"acceptable"`
}

// K 系数(均值极差法)
var (
	k1 = map[int]float64{2: 0.8862, 3: 0.5908} // 试验次数
	k2 = map[int]float64{2: 0.7071, 3: 0.5231} // 评价人数
	k3 = map[int]float64{                      // 零件数
		2: 0.7071, 3: 0.5231, 4: 0.4467, 5: 0.4030, 6: 0.3742,
		7: 0.3534, 8: 0.3375, 9: 0.3249, 10: 0.3146,
	}
)

// GageRR 均值极差法 Gage R&R
func GageRR(in GageRRInput) GageRRResult {
	o, p, t := len(in.Data), len(in.Data[0]), len(in.Data[0][0])
	res := GageRRResult{Parts: p, Operators: o, Trials: t}

	// 重复性: 所有 (op,part) 单元极差的均值
	var rSum float64
	for op := 0; op < o; op++ {
		for pt := 0; pt < p; pt++ {
			lo, hi := in.Data[op][pt][0], in.Data[op][pt][0]
			for tr := 1; tr < t; tr++ {
				v := in.Data[op][pt][tr]
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
			rSum += hi - lo
		}
	}
	rBar := rSum / float64(o*p)
	res.EV = rBar * k1[t]

	// 再现性: 各评价人均值极差
	opMeans := make([]float64, o)
	for op := 0; op < o; op++ {
		s := 0.0
		for pt := 0; pt < p; pt++ {
			for tr := 0; tr < t; tr++ {
				s += in.Data[op][pt][tr]
			}
		}
		opMeans[op] = s / float64(p*t)
	}
	xDiff := maxf(opMeans) - minf(opMeans)
	av2 := math.Pow(xDiff*k2[o], 2) - math.Pow(res.EV, 2)/float64(p*t)
	if av2 > 0 {
		res.AV = math.Sqrt(av2)
	}
	res.GRR = math.Sqrt(res.EV*res.EV + res.AV*res.AV)

	// 零件变差: 各零件均值极差
	partMeans := make([]float64, p)
	for pt := 0; pt < p; pt++ {
		s := 0.0
		for op := 0; op < o; op++ {
			for tr := 0; tr < t; tr++ {
				s += in.Data[op][pt][tr]
			}
		}
		partMeans[pt] = s / float64(o*t)
	}
	rp := maxf(partMeans) - minf(partMeans)
	res.PV = rp * k3[p]
	res.TV = math.Sqrt(res.GRR*res.GRR + res.PV*res.PV)

	if res.TV > 0 {
		res.PctEV = 100 * res.EV / res.TV
		res.PctAV = 100 * res.AV / res.TV
		res.PctGRR = 100 * res.GRR / res.TV
		res.PctPV = 100 * res.PV / res.TV
	}
	if res.GRR > 0 {
		res.NDC = int(1.41 * res.PV / res.GRR)
	}
	switch {
	case res.PctGRR < 10 && res.NDC >= 5:
		res.Acceptable = "可接受"
	case res.PctGRR < 30 && res.NDC >= 5:
		res.Acceptable = "有条件接受(按应用重要性)"
	default:
		res.Acceptable = "不可接受, 需改进测量系统"
	}
	return res
}

func maxf(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func minf(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x < m {
			m = x
		}
	}
	return m
}
