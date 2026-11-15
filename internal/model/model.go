package model

import "time"

// ChartType 控制图类型
type ChartType string

const (
	XbarR  ChartType = "xbar-r" // 均值-极差图
	XbarS  ChartType = "xbar-s" // 均值-标准差图
	IMR    ChartType = "i-mr"   // 单值-移动极差图
	PChart ChartType = "p"      // 不良率图
)

// Process 工序
type Process struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Characteristic 质量特性(关键尺寸/不良率等)
type Characteristic struct {
	ID           string    `json:"id"`
	ProcessID    string    `json:"processId"`
	Name         string    `json:"name"`
	Unit         string    `json:"unit"`
	USL          float64   `json:"usl"`
	LSL          float64   `json:"lsl"`
	Target       float64   `json:"target"`
	Chart        ChartType `json:"chart"`
	SubgroupSize int       `json:"subgroupSize"` // 计量型子组大小
	LotSize      int       `json:"lotSize"`      // p图每批检验数
	Rules        [8]bool   `json:"rules"`        // 八条判异准则开关
}

// Machine 机台
type Machine struct {
	ID        string `json:"id"`
	ProcessID string `json:"processId"`
	Name      string `json:"name"`
}

// Measurement 一条测量记录(p图用 Defectives/Inspected)
type Measurement struct {
	ID         int64     `json:"id"`
	CharID     string    `json:"charId"`
	MachineID  string    `json:"machineId"`
	Time       time.Time `json:"time"`
	Value      float64   `json:"value"`
	Defectives int       `json:"defectives,omitempty"`
	Inspected  int       `json:"inspected,omitempty"`
	Batch      string    `json:"batch"`
	Shift      string    `json:"shift"`
	Flag       string    `json:"flag,omitempty"` // unit_corrected 等
}

// Point 控制图上的一个描点(子组统计量)
type Point struct {
	Index      int       `json:"index"`
	Time       time.Time `json:"time"`
	Value      float64   `json:"value"`      // 均值/单值/不良率
	Dispersion float64   `json:"dispersion"` // R / S / MR
	Batch      string    `json:"batch"`
	Shift      string    `json:"shift"`
	N          int       `json:"n"`
}

// Limits 控制限(由稳定期数据计算)
type Limits struct {
	Active  bool      `json:"active"`
	CL      float64   `json:"cl"`
	UCL     float64   `json:"ucl"`
	LCL     float64   `json:"lcl"`
	DCL     float64   `json:"dcl"`   // 离散图(R/S/MR)中心线
	DUCL    float64   `json:"ducl"`  // 离散图上控制限
	DLCL    float64   `json:"dlcl"`  // 离散图下控制限
	Sigma   float64   `json:"sigma"` // 组内标准差估计
	From    time.Time `json:"from"`
	Samples int       `json:"samples"` // 用于计算的子组数
	Reason  string    `json:"reason"`  // 计算缘由: 初始稳定期 / 换模 / 换料
}

// Alarm 判异报警
type Alarm struct {
	ID            int64     `json:"id"`
	CharID        string    `json:"charId"`
	MachineID     string    `json:"machineId"`
	Rule          int       `json:"rule"` // 1-8
	RuleName      string    `json:"ruleName"`
	Detail        string    `json:"detail"`
	Time          time.Time `json:"time"`
	Status        string    `json:"status"` // open / action / effective / closed
	Action        string    `json:"action,omitempty"`
	ActionBy      string    `json:"actionBy,omitempty"`
	ActionTime    time.Time `json:"actionTime,omitempty"`
	EffectiveTime time.Time `json:"effectiveTime,omitempty"`
	ClosedTime    time.Time `json:"closedTime,omitempty"`
	GoodCount     int       `json:"goodCount"` // 措施后连续正常子组数
}

// RecalcEvent 换模/换料触发的控制限重算事件
type RecalcEvent struct {
	ID        int64     `json:"id"`
	CharID    string    `json:"charId"`
	MachineID string    `json:"machineId"`
	Reason    string    `json:"reason"` // 换模 / 换料
	Time      time.Time `json:"time"`
	Done      bool      `json:"done"`
	DoneTime  time.Time `json:"doneTime,omitempty"`
}

// QualityStats 数据质量过滤统计
type QualityStats struct {
	Received       int64 `json:"received"`
	Missing        int64 `json:"missing"`        // 缺失
	UnitCorrected  int64 `json:"unitCorrected"`  // 单位录错(已纠正)
	OutlierDropped int64 `json:"outlierDropped"` // 明显录入错误(已丢弃)
}
