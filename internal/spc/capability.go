package spc

// Capability 过程能力指数结果
type Capability struct {
	N           int     `json:"n"`
	Mean        float64 `json:"mean"`
	StdOverall  float64 `json:"stdOverall"` // 总体标准差(Pp/Ppk 用)
	StdWithin   float64 `json:"stdWithin"`  // 组内标准差(Cp/Cpk 用)
	Cp          float64 `json:"cp"`
	Cpk         float64 `json:"cpk"`
	Pp          float64 `json:"pp"`
	Ppk         float64 `json:"ppk"`
	OutOfSpecN  int     `json:"outOfSpecN"`
	OutOfSpecPc float64 `json:"outOfSpecPct"`
}

// CapabilityIndices 计算 Cp/Cpk(组内σ) 与 Pp/Ppk(总体σ)
func CapabilityIndices(vals []float64, usl, lsl, sigmaWithin float64) Capability {
	c := Capability{N: len(vals)}
	if len(vals) == 0 {
		return c
	}
	m := mean(vals)
	c.Mean = m
	c.StdOverall = StdDev(vals)
	c.StdWithin = sigmaWithin
	for _, v := range vals {
		if v > usl || v < lsl {
			c.OutOfSpecN++
		}
	}
	c.OutOfSpecPc = 100 * float64(c.OutOfSpecN) / float64(len(vals))
	tol := usl - lsl
	if sigmaWithin > 0 {
		c.Cp = tol / (6 * sigmaWithin)
		c.Cpk = min((usl-m)/(3*sigmaWithin), (m-lsl)/(3*sigmaWithin))
	}
	if c.StdOverall > 0 {
		c.Pp = tol / (6 * c.StdOverall)
		c.Ppk = min((usl-m)/(3*c.StdOverall), (m-lsl)/(3*c.StdOverall))
	}
	return c
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
