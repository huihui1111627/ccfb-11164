package generator

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"spc/internal/core"
	"spc/internal/model"
	"spc/internal/spc"
)

// Event 注入的失控事件(真值)
type Event struct {
	ID          int       `json:"id"`
	Type        string    `json:"type"` // mean_shift / variance_up / periodic / rate_shift
	CharID      string    `json:"charId"`
	MachineID   string    `json:"machineId"`
	Start       time.Time `json:"start"`
	Desc        string    `json:"desc"`
	Detected    bool      `json:"detected"`
	DetectTime  time.Time `json:"detectTime,omitempty"`
	DelayMinute float64   `json:"delayMinutes,omitempty"`
	ExpectAlarm bool      `json:"expectAlarm"` // false=过程搬迁(换模), 预期不报警
}

type anomaly struct {
	typ       string
	meanShift float64 // 以σ为单位
	varMult   float64
	period    float64 // 以子组为单位
	ampl      float64
	rate      float64 // p图不良率
	start     time.Time
}

// Generator 测量数据生成器
type Generator struct {
	e       *core.Engine
	rnd     *rand.Rand
	mu      sync.Mutex
	simTime time.Time
	step    time.Duration // 每 tick 推进的模拟时长
	events  []*Event
	active  map[string][]*anomaly // key: char|machine
	// 评估统计
	totalAlarms int
	trueAlarms  int
	falseAlarms int
	stableSub   int64
	recalcFired bool
	simStart    time.Time
	machines    []model.Machine
	chars       []model.Characteristic
}

func New(e *core.Engine, seed int64, step time.Duration) *Generator {
	g := &Generator{
		e:       e,
		rnd:     rand.New(rand.NewSource(seed)),
		simTime: time.Date(2026, 11, 16, 8, 0, 0, 0, time.Local),
		step:    step,
		active:  map[string][]*anomaly{},
	}
	g.simStart = g.simTime
	g.setup()
	e.OnAlarm = g.onAlarm
	e.OnAction = g.onAction
	return g
}

func (g *Generator) setup() {
	g.e.AddProcess(model.Process{ID: "P1", Name: "注塑成型"})
	allOn := [8]bool{true, true, true, true, true, true, true, true}
	chars := []model.Characteristic{
		{ID: "C1", ProcessID: "P1", Name: "关键尺寸 Ø25", Unit: "mm",
			USL: 25.10, LSL: 24.90, Target: 25.00, Chart: model.XbarR, SubgroupSize: 5, Rules: allOn},
		{ID: "C2", ProcessID: "P1", Name: "产品重量", Unit: "g",
			USL: 85.60, LSL: 84.40, Target: 85.00, Chart: model.XbarS, SubgroupSize: 5, Rules: allOn},
		{ID: "C3", ProcessID: "P1", Name: "浇口高度", Unit: "mm",
			USL: 3.35, LSL: 3.05, Target: 3.20, Chart: model.IMR, SubgroupSize: 1, Rules: allOn},
		{ID: "C4", ProcessID: "P1", Name: "外观不良率", Unit: "%",
			USL: 0.05, LSL: 0, Target: 0.01, Chart: model.PChart, LotSize: 200, Rules: allOn},
	}
	for _, c := range chars {
		g.e.AddCharacteristic(c)
		g.chars = append(g.chars, c)
	}
	for i := 1; i <= 8; i++ {
		m := model.Machine{ID: fmt.Sprintf("M-%02d", i), ProcessID: "P1",
			Name: fmt.Sprintf("%d号注塑机", i)}
		g.e.AddMachine(m)
		g.machines = append(g.machines, m)
	}
}

// ScheduleEvents 预排失控事件(起点即真值; 均安排在初始稳定期~2h之后)
func (g *Generator) ScheduleEvents() {
	h := time.Hour
	g.schedule("mean_shift", "C1", "M-03", 4*h, "均值漂移 +1.5σ(模温缓慢升高)", &anomaly{typ: "mean_shift", meanShift: 1.5})
	g.schedule("variance_up", "C1", "M-05", 6*h, "方差变大 ×2.2(液压不稳)", &anomaly{typ: "variance_up", varMult: 2.2})
	g.schedule("periodic", "C2", "M-02", 8*h, "周期性波动 幅1.2σ 周期30子组(环境温度)", &anomaly{typ: "periodic", ampl: 1.2, period: 30})
	g.schedule("rate_shift", "C4", "M-04", 10*h, "不良率 1%→4%(模具磨损)", &anomaly{typ: "rate_shift", rate: 0.04})
	g.schedule("mean_shift", "C3", "M-07", 12*h, "均值漂移 -1.2σ(喷嘴磨损)", &anomaly{typ: "mean_shift", meanShift: -1.2})
	// 换模演示: 14h 后 M-03/C1 换模, 过程搬到新均值并触发控制限重算(预期不报警)
	g.scheduleNoAlarm("mean_shift", "C1", "M-03", 14*h, "换模后新稳定过程 +0.4σ", &anomaly{typ: "mean_shift", meanShift: -1.1}) // 净效果 +0.4σ
}

func (g *Generator) schedule(typ, charID, machineID string, after time.Duration, desc string, a *anomaly) {
	g.sched(typ, charID, machineID, after, desc, a, true)
}

func (g *Generator) scheduleNoAlarm(typ, charID, machineID string, after time.Duration, desc string, a *anomaly) {
	g.sched(typ, charID, machineID, after, desc, a, false)
}

func (g *Generator) sched(typ, charID, machineID string, after time.Duration, desc string, a *anomaly, expect bool) {
	start := g.simTime.Add(after)
	a.start = start
	g.events = append(g.events, &Event{
		ID: len(g.events) + 1, Type: typ, CharID: charID, MachineID: machineID,
		Start: start, Desc: desc, ExpectAlarm: expect,
	})
	k := charID + "|" + machineID
	g.active[k] = append(g.active[k], a)
}

// SimTime 当前模拟时间
func (g *Generator) SimTime() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.simTime
}

// Start 启动生成循环
func (g *Generator) Start() {
	ticker := time.NewTicker(time.Second)
	go func() {
		for range ticker.C {
			g.tick()
		}
	}()
}

func (g *Generator) tick() {
	g.mu.Lock()
	g.simTime = g.simTime.Add(g.step)
	now := g.simTime
	g.mu.Unlock()

	// 换模演示: 14h 后(22:00)触发控制限重算
	if now.Hour() == 22 && now.Minute() < 5 && !g.recalcFired {
		g.recalcFired = true
		g.e.Recalc("C1", "M-03", "换模", now)
	}

	for _, m := range g.machines {
		for _, c := range g.chars {
			g.produce(c, m, now)
		}
	}
}

func (g *Generator) produce(c model.Characteristic, m model.Machine, now time.Time) {
	k := c.ID + "|" + m.ID
	batch := fmt.Sprintf("B%s-%d", now.Format("0102"), now.Hour()/8+1)
	shift := shiftOf(now)

	g.mu.Lock()
	// 稳定期子组计数(用于误报率)
	stable := true
	for _, a := range g.active[k] {
		if !now.Before(a.start) {
			stable = false
			break
		}
	}
	if stable {
		g.stableSub++
	}
	ans := append([]*anomaly(nil), g.active[k]...)
	g.mu.Unlock()

	if c.Chart == model.PChart {
		rate := 0.01
		for _, a := range ans {
			if now.Before(a.start) {
				continue
			}
			if a.typ == "rate_shift" {
				rate = a.rate
			}
		}
		def := 0
		for i := 0; i < c.LotSize; i++ {
			if g.rnd.Float64() < rate {
				def++
			}
		}
		g.e.IngestP(c.ID, m.ID, def, c.LotSize, now, batch, shift)
		return
	}

	// 计量型: 基准 σ 取公差带的 1/8 (稳定期 Cp≈1.33)
	sigma := (c.USL - c.LSL) / 8
	mean := c.Target
	for i := 0; i < c.SubgroupSize; i++ {
		mu, sg := mean, sigma
		for _, a := range ans {
			if now.Before(a.start) {
				continue
			}
			switch a.typ {
			case "mean_shift":
				mu += a.meanShift * sigma
			case "variance_up":
				sg *= a.varMult
			case "periodic":
				phase := float64(now.Sub(g.simStart)) / float64(g.step) // 子组序号
				mu += a.ampl * sigma * math.Sin(2*math.Pi*phase/a.period)
			}
		}
		v := g.rnd.NormFloat64()*sg + mu
		// 注入数据质量问题: 1%缺失, 0.5%单位错(×10), 0.3%明显错误
		r := g.rnd.Float64()
		switch {
		case r < 0.01:
			v = math.NaN()
		case r < 0.015:
			v = v * 10
		case r < 0.018:
			v = v + 50*sigma
		}
		g.e.Ingest(c.ID, m.ID, v, now, batch, shift)
	}
}

func shiftOf(t time.Time) string {
	switch {
	case t.Hour() >= 8 && t.Hour() < 16:
		return "白班"
	case t.Hour() >= 16 && t.Hour() < 24:
		return "中班"
	default:
		return "夜班"
	}
}

// onAlarm 报警回调: 匹配真值事件, 统计及时性与误报
func (g *Generator) onAlarm(a *model.Alarm) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalAlarms++
	matched := false
	for _, ev := range g.events {
		if ev.CharID != a.CharID || ev.MachineID != a.MachineID {
			continue
		}
		if !a.Time.Before(ev.Start) {
			matched = true
			if !ev.Detected {
				ev.Detected = true
				ev.DetectTime = a.Time
				ev.DelayMinute = a.Time.Sub(ev.Start).Minutes()
			}
		}
	}
	if matched {
		g.trueAlarms++
	} else {
		g.falseAlarms++
	}
}

// onAction 措施提交回调: 模拟维修生效, 移除该机台该特性的失控注入
func (g *Generator) onAction(a *model.Alarm) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := a.CharID + "|" + a.MachineID
	kept := g.active[k][:0]
	for _, an := range g.active[k] {
		if an.start.Before(a.ActionTime) || an.start.Equal(a.ActionTime) {
			continue // 维修排除了这个异常
		}
		kept = append(kept, an)
	}
	g.active[k] = kept
}

// EvalReport 仿真评估报告
type EvalReport struct {
	Events          []Event `json:"events"`
	TotalAlarms     int     `json:"totalAlarms"`
	TrueAlarms      int     `json:"trueAlarms"`
	FalseAlarms     int     `json:"falseAlarms"`
	StableSubgroups int64   `json:"stableSubgroups"`
	FalseAlarmRate  float64 `json:"falseAlarmRatePer100"` // 每100稳定子组误报数
	Detected        int     `json:"detected"`
	EventTotal      int     `json:"eventTotal"`
	AvgDelayMinutes float64 `json:"avgDelayMinutes"`
}

func (g *Generator) Report() EvalReport {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := EvalReport{TotalAlarms: g.totalAlarms, TrueAlarms: g.trueAlarms,
		FalseAlarms: g.falseAlarms, StableSubgroups: g.stableSub}
	var delaySum float64
	for _, ev := range g.events {
		r.Events = append(r.Events, *ev)
		if !ev.ExpectAlarm {
			continue
		}
		r.EventTotal++
		if ev.Detected {
			r.Detected++
			delaySum += ev.DelayMinute
		}
	}
	if r.Detected > 0 {
		r.AvgDelayMinutes = delaySum / float64(r.Detected)
	}
	if g.stableSub > 0 {
		r.FalseAlarmRate = 100 * float64(g.falseAlarms) / float64(g.stableSub)
	}
	return r
}

// RunMSA 生成一次 Gage R&R 研究(10零件×3评价人×3次)并计算
func (g *Generator) RunMSA(charID string) (spc.GageRRResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var c *model.Characteristic
	for _, ch := range g.chars {
		if ch.ID == charID {
			ch2 := ch
			c = &ch2
		}
	}
	if c == nil || c.Chart == model.PChart {
		return spc.GageRRResult{}, fmt.Errorf("特性不存在或不适用计量型MSA")
	}
	parts, ops, trials := 10, 3, 3
	tol := c.USL - c.LSL
	repeatSigma := tol / 60 // 重复性 ~10%公差/6
	opBias := []float64{0, tol / 80, -tol / 100}
	trueVals := make([]float64, parts)
	for i := range trueVals {
		trueVals[i] = c.LSL + tol*(0.1+0.8*float64(i)/float64(parts-1))
	}
	data := make([][][]float64, ops)
	names := []string{"张三", "李四", "王五"}
	labels := make([]string, parts)
	for p := 0; p < parts; p++ {
		labels[p] = fmt.Sprintf("零件%02d", p+1)
	}
	for o := 0; o < ops; o++ {
		data[o] = make([][]float64, parts)
		for p := 0; p < parts; p++ {
			data[o][p] = make([]float64, trials)
			for t := 0; t < trials; t++ {
				data[o][p][t] = trueVals[p] + opBias[o] + g.rnd.NormFloat64()*repeatSigma
			}
		}
	}
	res := spc.GageRR(spc.GageRRInput{Data: data, Operators: names, PartLabels: labels})
	return res, nil
}
