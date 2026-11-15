package spc

import "fmt"

// RuleNames 八条判异准则(Nelson/WECO)
var RuleNames = [9]string{
	"",
	"准则1: 单点超出3σ控制限",
	"准则2: 连续9点落在中心线同侧",
	"准则3: 连续6点持续上升或下降",
	"准则4: 连续14点上下交替",
	"准则5: 连续3点中有2点超出同侧2σ",
	"准则6: 连续5点中有4点超出同侧1σ",
	"准则7: 连续15点落在中心线两侧1σ以内",
	"准则8: 连续8点落在中心线两侧且均超出1σ",
}

// RuleHit 一次判异命中
type RuleHit struct {
	Rule   int
	Name   string
	Detail string
}

// ZPoint 带标准化信息的描点
type ZPoint struct {
	Index int
	Z     float64 // (value-CL)/sigma
	Side  int     // +1 中心线上方, -1 下方, 0 线上
}

// EvalRules 对最新一个点(pts 末尾)逐条评估已启用的判异准则
func EvalRules(pts []ZPoint, enabled [8]bool) []RuleHit {
	var hits []RuleHit
	n := len(pts)
	if n == 0 {
		return nil
	}
	add := func(rule int, detail string) {
		hits = append(hits, RuleHit{Rule: rule, Name: RuleNames[rule], Detail: detail})
	}
	last := pts[n-1]

	// 准则1: 一点超出 ±3σ
	if enabled[0] && (last.Z > 3 || last.Z < -3) {
		side := "上"
		if last.Z < 0 {
			side = "下"
		}
		add(1, fmt.Sprintf("第%d点 z=%.2f, 超出%s控制限", last.Index, last.Z, side))
	}

	// 准则2: 连续9点同侧
	if enabled[1] && n >= 9 {
		side := pts[n-1].Side
		if side != 0 {
			k := 0
			for i := n - 1; i >= 0 && pts[i].Side == side; i-- {
				k++
			}
			if k >= 9 {
				s := "上"
				if side < 0 {
					s = "下"
				}
				add(2, fmt.Sprintf("第%d~%d点连续%d点在中心线%s侧", pts[n-k].Index, last.Index, k, s))
			}
		}
	}

	// 准则3: 连续6点单调上升/下降
	if enabled[2] && n >= 6 {
		up, down := 1, 1
		for i := n - 1; i > 0; i-- {
			if pts[i].Z > pts[i-1].Z {
				up++
			} else {
				break
			}
		}
		for i := n - 1; i > 0; i-- {
			if pts[i].Z < pts[i-1].Z {
				down++
			} else {
				break
			}
		}
		if up >= 6 {
			add(3, fmt.Sprintf("第%d~%d点连续%d点持续上升", pts[n-up].Index, last.Index, up))
		} else if down >= 6 {
			add(3, fmt.Sprintf("第%d~%d点连续%d点持续下降", pts[n-down].Index, last.Index, down))
		}
	}

	// 准则4: 连续14点上下交替
	if enabled[3] && n >= 14 {
		k := 1
		for i := n - 1; i > 0; i-- {
			d1 := pts[i].Z - pts[i-1].Z
			if i == n-1 {
				if d1 == 0 {
					break
				}
				k++
				continue
			}
			d2 := pts[i+1].Z - pts[i].Z
			if d1*d2 < 0 {
				k++
			} else {
				break
			}
		}
		if k >= 14 {
			add(4, fmt.Sprintf("第%d~%d点连续%d点上下交替", pts[n-k].Index, last.Index, k))
		}
	}

	// 准则5: 3点中2点超同侧2σ
	if enabled[4] && n >= 3 {
		seg := pts[n-3:]
		for _, side := range []int{1, -1} {
			c := 0
			for _, p := range seg {
				if side > 0 && p.Z > 2 {
					c++
				}
				if side < 0 && p.Z < -2 {
					c++
				}
			}
			if c >= 2 {
				s := "上"
				if side < 0 {
					s = "下"
				}
				add(5, fmt.Sprintf("第%d~%d点中%d点超出%s2σ", seg[0].Index, last.Index, c, s))
				break
			}
		}
	}

	// 准则6: 5点中4点超同侧1σ
	if enabled[5] && n >= 5 {
		seg := pts[n-5:]
		for _, side := range []int{1, -1} {
			c := 0
			for _, p := range seg {
				if side > 0 && p.Z > 1 {
					c++
				}
				if side < 0 && p.Z < -1 {
					c++
				}
			}
			if c >= 4 {
				s := "上"
				if side < 0 {
					s = "下"
				}
				add(6, fmt.Sprintf("第%d~%d点中%d点超出%s1σ", seg[0].Index, last.Index, c, s))
				break
			}
		}
	}

	// 准则7: 连续15点在±1σ内(分层)
	if enabled[6] && n >= 15 {
		k := 0
		for i := n - 1; i >= 0 && pts[i].Z < 1 && pts[i].Z > -1; i-- {
			k++
		}
		if k >= 15 {
			add(7, fmt.Sprintf("第%d~%d点连续%d点落在±1σ内, 疑似分层/控制限过宽", pts[n-k].Index, last.Index, k))
		}
	}

	// 准则8: 连续8点在±1σ外(混合, 可两侧)
	if enabled[7] && n >= 8 {
		k := 0
		for i := n - 1; i >= 0 && (pts[i].Z > 1 || pts[i].Z < -1); i-- {
			k++
		}
		if k >= 8 {
			add(8, fmt.Sprintf("第%d~%d点连续%d点超出±1σ, 疑似两总体混合", pts[n-k].Index, last.Index, k))
		}
	}

	return hits
}
