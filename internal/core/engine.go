package core

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"spc/internal/model"
	"spc/internal/spc"
)

const (
	initialSubgroups = 25 // 初始稳定期子组数
	recalcSubgroups  = 20 // 换模/换料后重算所需子组数
	effectiveNeed    = 5  // 措施生效需连续正常子组数
)

// Violation 描点级判异记录(看板标红用)
type Violation struct {
	Index  int       `json:"index"`
	Time   time.Time `json:"time"`
	Rule   int       `json:"rule"`
	Detail string    `json:"detail"`
}

type series struct {
	char        *model.Characteristic
	machineID   string
	pending     []model.Measurement
	meas        []model.Measurement // 已接收的原始测量(能力分析用)
	points      []model.Point
	limits      model.Limits
	collectFrom int  // 重算时从该点起收集
	collecting  bool // 正在收集稳定期数据
	recalcWhy   string
	zhist       []spc.ZPoint // 控制限生效后的 z 序列
	violations  []Violation
}

// Engine SPC 引擎
type Engine struct {
	mu         sync.Mutex
	processes  []model.Process
	chars      map[string]*model.Characteristic
	machines   map[string]model.Machine
	series     map[string]*series
	alarms     []*model.Alarm
	recalcEvts []*model.RecalcEvent
	quality    model.QualityStats
	nextMeasID int64
	nextAlmID  int64
	nextEvtID  int64
	// OnAlarm 报警回调(生成器用来评估及时性/误报)
	OnAlarm func(a *model.Alarm)
	// OnAction 措施提交回调(生成器用来模拟维修生效)
	OnAction func(a *model.Alarm)
}

func NewEngine() *Engine {
	return &Engine{
		chars:    map[string]*model.Characteristic{},
		machines: map[string]model.Machine{},
		series:   map[string]*series{},
	}
}

func (e *Engine) AddProcess(p model.Process) { e.processes = append(e.processes, p) }

func (e *Engine) AddCharacteristic(c model.Characteristic) {
	cp := c
	e.chars[c.ID] = &cp
}

func (e *Engine) AddMachine(m model.Machine) { e.machines[m.ID] = m }

func skey(charID, machineID string) string { return charID + "|" + machineID }

func (e *Engine) getSeries(charID, machineID string) *series {
	k := skey(charID, machineID)
	s, ok := e.series[k]
	if !ok {
		c := e.chars[charID]
		s = &series{char: c, machineID: machineID, collecting: true, recalcWhy: "初始稳定期"}
		e.series[k] = s
	}
	return s
}

// validate 数据质量过滤: 缺失/单位错误/明显录入错误
func (e *Engine) validate(c *model.Characteristic, v float64) (float64, string, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		e.quality.Missing++
		return 0, "missing", false
	}
	tol := (c.USL - c.LSL) / 2
	if math.Abs(v-c.Target) <= 4*tol {
		return v, "", true
	}
	// 疑似单位录错: 尝试 ×10 /÷10 /英寸↔毫米 纠回规格附近
	for _, f := range []float64{0.1, 10, 25.4, 1 / 25.4} {
		cv := v * f
		if math.Abs(cv-c.Target) <= 2*tol {
			e.quality.UnitCorrected++
			return cv, "unit_corrected", true
		}
	}
	e.quality.OutlierDropped++
	return 0, "outlier", false
}

// Ingest 接收一条计量型测量
func (e *Engine) Ingest(charID, machineID string, value float64, t time.Time, batch, shift string) (bool, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.chars[charID]
	if !ok {
		return false, "unknown characteristic"
	}
	e.quality.Received++
	v, flag, ok := e.validate(c, value)
	if !ok {
		return false, flag
	}
	e.nextMeasID++
	m := model.Measurement{
		ID: e.nextMeasID, CharID: charID, MachineID: machineID,
		Time: t, Value: v, Batch: batch, Shift: shift, Flag: flag,
	}
	s := e.getSeries(charID, machineID)
	s.meas = append(s.meas, m)

	if c.Chart == model.IMR {
		s.pending = append(s.pending, m)
		e.closeSubgroup(s)
		return true, flag
	}
	s.pending = append(s.pending, m)
	if len(s.pending) >= c.SubgroupSize {
		e.closeSubgroup(s)
	}
	return true, flag
}

// IngestP 接收一条计数型(p图)检验记录
func (e *Engine) IngestP(charID, machineID string, defectives, inspected int, t time.Time, batch, shift string) (bool, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.chars[charID]; !ok {
		return false, "unknown characteristic"
	}
	e.quality.Received++
	if inspected <= 0 || defectives < 0 || defectives > inspected {
		e.quality.Missing++
		return false, "invalid count"
	}
	e.nextMeasID++
	m := model.Measurement{
		ID: e.nextMeasID, CharID: charID, MachineID: machineID, Time: t,
		Defectives: defectives, Inspected: inspected, Batch: batch, Shift: shift,
	}
	s := e.getSeries(charID, machineID)
	s.meas = append(s.meas, m)
	s.pending = append(s.pending, m)
	e.closeSubgroup(s)
	return true, ""
}

// closeSubgroup 完成一个子组: 计算描点、必要时重算控制限、判异
func (e *Engine) closeSubgroup(s *series) {
	c := s.char
	var p model.Point
	p.Index = len(s.points) + 1
	p.Time = s.pending[len(s.pending)-1].Time
	p.Batch = s.pending[len(s.pending)-1].Batch
	p.Shift = s.pending[len(s.pending)-1].Shift
	p.N = len(s.pending)

	switch c.Chart {
	case model.XbarR, model.XbarS:
		sum, lo, hi := 0.0, math.Inf(1), math.Inf(-1)
		for _, m := range s.pending {
			sum += m.Value
			lo = math.Min(lo, m.Value)
			hi = math.Max(hi, m.Value)
		}
		p.Value = sum / float64(len(s.pending))
		if c.Chart == model.XbarR {
			p.Dispersion = hi - lo
		} else {
			vs := make([]float64, len(s.pending))
			for i, m := range s.pending {
				vs[i] = m.Value
			}
			p.Dispersion = spc.StdDev(vs)
		}
	case model.IMR:
		p.Value = s.pending[0].Value
		if len(s.points) > 0 {
			p.Dispersion = math.Abs(p.Value - s.points[len(s.points)-1].Value)
		}
	case model.PChart:
		def, insp := 0, 0
		for _, m := range s.pending {
			def += m.Defectives
			insp += m.Inspected
		}
		p.Value = float64(def) / float64(insp)
		p.N = insp
	}
	s.pending = s.pending[:0]
	s.points = append(s.points, p)

	// 控制限(重)算
	if s.collecting {
		n := len(s.points) - s.collectFrom
		need := initialSubgroups
		if s.recalcWhy != "初始稳定期" {
			need = recalcSubgroups
		}
		if n >= need {
			e.computeLimits(s)
			s.collecting = false
		} else {
			return
		}
	}
	if !s.limits.Active {
		return
	}

	// 判异
	sig := (s.limits.UCL - s.limits.CL) / 3
	if c.Chart == model.PChart && p.N > 0 {
		sig = math.Sqrt(s.limits.CL * (1 - s.limits.CL) / float64(p.N))
	}
	z := 0.0
	if sig > 0 {
		z = (p.Value - s.limits.CL) / sig
	}
	side := 0
	if z > 0 {
		side = 1
	} else if z < 0 {
		side = -1
	}
	s.zhist = append(s.zhist, spc.ZPoint{Index: p.Index, Z: z, Side: side})

	hits := spc.EvalRules(s.zhist, c.Rules)
	// 离散图(R/S/MR)超限
	if c.Chart != model.PChart && p.Dispersion > s.limits.DUCL && p.Index > 1 {
		hits = append(hits, spc.RuleHit{Rule: 1, Name: spc.RuleNames[1],
			Detail: fmt.Sprintf("第%d点离散图(R/S/MR)值%.4g超出上控制限%.4g, 波动异常增大", p.Index, p.Dispersion, s.limits.DUCL)})
	}
	for _, h := range hits {
		s.violations = append(s.violations, Violation{Index: p.Index, Time: p.Time, Rule: h.Rule, Detail: h.Detail})
		e.raiseAlarm(s, h, p.Time)
	}
	e.updateEffectiveness(s, len(hits) == 0, p.Time)
}

func (e *Engine) computeLimits(s *series) {
	c := s.char
	pts := s.points[s.collectFrom:]
	switch c.Chart {
	case model.XbarR, model.XbarS:
		means := make([]float64, len(pts))
		disp := make([]float64, len(pts))
		for i, p := range pts {
			means[i], disp[i] = p.Value, p.Dispersion
		}
		if c.Chart == model.XbarR {
			s.limits = spc.XbarRLimits(means, disp, c.SubgroupSize, s.recalcWhy)
		} else {
			s.limits = spc.XbarSLimits(means, disp, c.SubgroupSize, s.recalcWhy)
		}
	case model.IMR:
		vals := make([]float64, len(pts))
		for i, p := range pts {
			vals[i] = p.Value
		}
		s.limits = spc.IMRLimits(vals, s.recalcWhy)
	case model.PChart:
		def, insp := 0, 0
		for _, p := range pts {
			insp += p.N
			def += int(math.Round(p.Value * float64(p.N)))
		}
		s.limits = spc.PLimits(def, insp, float64(insp)/float64(len(pts)), s.recalcWhy)
	}
	s.limits.From = pts[0].Time
	s.zhist = nil
	// 完成重算事件
	for _, ev := range e.recalcEvts {
		if !ev.Done && ev.CharID == s.char.ID && ev.MachineID == s.machineID {
			ev.Done = true
			ev.DoneTime = s.points[len(s.points)-1].Time
		}
	}
}

func (e *Engine) raiseAlarm(s *series, h spc.RuleHit, t time.Time) {
	// 同特性同机台同准则已有未关闭报警则不重复开
	for _, a := range e.alarms {
		if a.CharID == s.char.ID && a.MachineID == s.machineID && a.Rule == h.Rule &&
			(a.Status == "open" || a.Status == "action") {
			return
		}
	}
	e.nextAlmID++
	a := &model.Alarm{
		ID: e.nextAlmID, CharID: s.char.ID, MachineID: s.machineID,
		Rule: h.Rule, RuleName: h.Name, Detail: h.Detail,
		Time: t, Status: "open",
	}
	e.alarms = append(e.alarms, a)
	if e.OnAlarm != nil {
		e.OnAlarm(a)
	}
}

// updateEffectiveness 措施生效判定: 措施后连续 effectiveNeed 个正常子组
func (e *Engine) updateEffectiveness(s *series, inControl bool, t time.Time) {
	for _, a := range e.alarms {
		if a.CharID != s.char.ID || a.MachineID != s.machineID || a.Status != "action" {
			continue
		}
		if !t.After(a.ActionTime) {
			continue
		}
		if inControl {
			a.GoodCount++
			if a.GoodCount >= effectiveNeed {
				a.Status = "effective"
				a.EffectiveTime = t
			}
		} else {
			a.GoodCount = 0
		}
	}
}

// Recalc 换模/换料后触发控制限重算
func (e *Engine) Recalc(charID, machineID, reason string, t time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.getSeries(charID, machineID)
	s.collecting = true
	s.collectFrom = len(s.points)
	s.recalcWhy = reason
	s.limits.Active = false
	s.zhist = nil
	e.nextEvtID++
	e.recalcEvts = append(e.recalcEvts, &model.RecalcEvent{
		ID: e.nextEvtID, CharID: charID, MachineID: machineID, Reason: reason, Time: t,
	})
	return nil
}

// SetRules 设置特性判异准则开关
func (e *Engine) SetRules(charID string, rules [8]bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.chars[charID]
	if !ok {
		return errors.New("unknown characteristic")
	}
	c.Rules = rules
	return nil
}

// AddAction 操作工填写处理措施
func (e *Engine) AddAction(alarmID int64, action, by string, t time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range e.alarms {
		if a.ID == alarmID {
			if a.Status != "open" {
				return fmt.Errorf("报警状态为 %s, 不能填写措施", a.Status)
			}
			a.Action, a.ActionBy, a.ActionTime = action, by, t
			a.Status = "action"
			a.GoodCount = 0
			if e.OnAction != nil {
				e.OnAction(a)
			}
			return nil
		}
	}
	return errors.New("alarm not found")
}

// CloseAlarm 措施生效后才能关闭
func (e *Engine) CloseAlarm(alarmID int64, t time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range e.alarms {
		if a.ID == alarmID {
			if a.Status != "effective" {
				return fmt.Errorf("措施尚未验证生效(当前状态 %s), 不能关闭", a.Status)
			}
			a.Status = "closed"
			a.ClosedTime = t
			return nil
		}
	}
	return errors.New("alarm not found")
}

// ---------- 查询 ----------

func (e *Engine) Processes() []model.Process { return e.processes }

func (e *Engine) Characteristics() []model.Characteristic {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.Characteristic, 0, len(e.chars))
	for _, c := range e.chars {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (e *Engine) Machines() []model.Machine {
	out := make([]model.Machine, 0, len(e.machines))
	for _, m := range e.machines {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SeriesView 控制图数据
type SeriesView struct {
	Points     []model.Point `json:"points"`
	Limits     model.Limits  `json:"limits"`
	Violations []Violation   `json:"violations"`
	Collecting bool          `json:"collecting"`
	Need       int           `json:"need"`
}

func (e *Engine) SeriesView(charID, machineID string, lastN int) SeriesView {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.getSeries(charID, machineID)
	pts := s.points
	if lastN > 0 && len(pts) > lastN {
		pts = pts[len(pts)-lastN:]
	}
	v := SeriesView{
		Points: pts, Limits: s.limits, Collecting: s.collecting,
		Violations: s.violations,
	}
	if s.collecting {
		if s.recalcWhy == "初始稳定期" {
			v.Need = initialSubgroups
		} else {
			v.Need = recalcSubgroups
		}
	}
	return v
}

func (e *Engine) Alarms(status string) []model.Alarm {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []model.Alarm{}
	for i := len(e.alarms) - 1; i >= 0; i-- {
		a := e.alarms[i]
		if status == "" || a.Status == status {
			out = append(out, *a)
		}
	}
	return out
}

func (e *Engine) RecalcEvents() []model.RecalcEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]model.RecalcEvent, len(e.recalcEvts))
	for i, ev := range e.recalcEvts {
		out[i] = *ev
	}
	return out
}

func (e *Engine) Quality() model.QualityStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.quality
}

// Capability 按批次或班次分组计算能力指数
func (e *Engine) Capability(charID, machineID, groupBy string) map[string]spc.Capability {
	e.mu.Lock()
	defer e.mu.Unlock()
	groups := map[string][]float64{}
	sigmas := map[string]float64{}
	for _, s := range e.series {
		if s.char.ID != charID || (machineID != "" && s.machineID != machineID) {
			continue
		}
		if s.limits.Active {
			sigmas[s.machineID] = s.limits.Sigma
		}
		for _, m := range s.meas {
			if m.Value == 0 && m.Flag == "" {
				continue
			}
			key := m.Batch
			if groupBy == "shift" {
				key = m.Shift
			}
			groups[key] = append(groups[key], m.Value)
		}
	}
	// 组内σ: 各机台控制限σ的平均
	sw := 0.0
	for _, v := range sigmas {
		sw += v
	}
	if len(sigmas) > 0 {
		sw /= float64(len(sigmas))
	}
	c := e.chars[charID]
	out := map[string]spc.Capability{}
	for k, vals := range groups {
		out[k] = spc.CapabilityIndices(vals, c.USL, c.LSL, sw)
	}
	return out
}

// MachineStat 多机对比行
type MachineStat struct {
	MachineID  string    `json:"machineId"`
	N          int       `json:"n"`
	Mean       float64   `json:"mean"`
	Std        float64   `json:"std"`
	Cpk        float64   `json:"cpk"`
	OpenAlarms int       `json:"openAlarms"`
	LastValue  float64   `json:"lastValue"`
	LastTime   time.Time `json:"lastTime"`
}

func (e *Engine) Compare(charID string) []MachineStat {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.chars[charID]
	out := []MachineStat{}
	for _, s := range e.series {
		if s.char.ID != charID || len(s.points) == 0 {
			continue
		}
		vals := make([]float64, len(s.points))
		for i, p := range s.points {
			vals[i] = p.Value
		}
		m := 0.0
		for _, v := range vals {
			m += v
		}
		m /= float64(len(vals))
		st := MachineStat{
			MachineID: s.machineID, N: len(vals), Mean: m,
			Std: spc.StdDev(vals), LastValue: vals[len(vals)-1],
			LastTime: s.points[len(s.points)-1].Time,
		}
		if s.limits.Active && s.limits.Sigma > 0 {
			st.Cpk = math.Min((c.USL-m)/(3*s.limits.Sigma), (m-c.LSL)/(3*s.limits.Sigma))
		}
		for _, a := range e.alarms {
			if a.CharID == charID && a.MachineID == s.machineID &&
				(a.Status == "open" || a.Status == "action") {
				st.OpenAlarms++
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MachineID < out[j].MachineID })
	return out
}

// Overview 看板总览: 每个 特性×机台 的状态
type OverviewItem struct {
	CharID     string    `json:"charId"`
	CharName   string    `json:"charName"`
	MachineID  string    `json:"machineId"`
	Status     string    `json:"status"` // ok / collecting / alarm
	OpenAlarms int       `json:"openAlarms"`
	LastValue  float64   `json:"lastValue"`
	LastTime   time.Time `json:"lastTime"`
	Cpk        float64   `json:"cpk"`
}

func (e *Engine) Overview() []OverviewItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []OverviewItem{}
	for _, s := range e.series {
		if len(s.points) == 0 {
			continue
		}
		it := OverviewItem{
			CharID: s.char.ID, CharName: s.char.Name, MachineID: s.machineID,
			LastValue: s.points[len(s.points)-1].Value,
			LastTime:  s.points[len(s.points)-1].Time,
			Status:    "ok",
		}
		if s.collecting {
			it.Status = "collecting"
		}
		if s.limits.Active && s.limits.Sigma > 0 {
			m := 0.0
			n := 0
			for i := len(s.points) - 1; i >= 0 && n < 25; i-- {
				m += s.points[i].Value
				n++
			}
			m /= float64(n)
			it.Cpk = math.Min((s.char.USL-m)/(3*s.limits.Sigma), (m-s.char.LSL)/(3*s.limits.Sigma))
		}
		for _, a := range e.alarms {
			if a.CharID == s.char.ID && a.MachineID == s.machineID &&
				(a.Status == "open" || a.Status == "action") {
				it.OpenAlarms++
				it.Status = "alarm"
			}
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CharID != out[j].CharID {
			return out[i].CharID < out[j].CharID
		}
		return out[i].MachineID < out[j].MachineID
	})
	return out
}
