# 注塑车间 SPC 统计过程控制系统

替代"质检员每小时纸笔抽检、出废品才发现机器漂移"的人工模式:测量数据实时进入系统,
自动判异、自动报警、车间看板实时展示,异常处理闭环留痕。

## 运行

```bash
go run ./cmd/spc            # 默认 :8080, 模拟时钟每秒推进 5 分钟
# 打开 http://localhost:8080
```

参数: `-addr :8080` 监听地址, `-step 5m` 每秒推进的模拟时长, `-seed 42` 随机种子。

## 功能对照

| 需求 | 实现 |
|---|---|
| 四种控制图按特性选择 | 均值-极差图(X̄-R)、均值-标准差图(X̄-S)、单值-移动极差图(I-MR)、不良率图(p图),特性配置里选 `chart` |
| 控制限用稳定期数据计算 | 初始收集 25 个子组后自动计算;换模/换料后(`POST /api/recalc` 或生成器演示)重新收集 20 个子组重算 |
| 八条判异准则逐条开关 | Nelson/WECO 八准则,按特性配置,看板"控制图"页可勾选;报警写清命中哪条、哪些点 |
| 数据质量过滤 | 缺失(NaN)丢弃;单位录错(×10、英寸↔毫米)自动纠正并计数;明显录入错误(超 4 倍半公差且无法纠正)丢弃;看板顶栏实时计数 |
| 过程能力按批次/班次 | Cp/Cpk(组内σ)与 Pp/Ppk(总体σ),`GET /api/capability?char=C1&by=batch|shift` |
| 异常处理闭环 | 报警 → 操作工填处理措施 → 措施后连续 5 个子组正常自动判定"生效" → 才能关闭 |
| 多机台同特性对比 | `GET /api/compare?char=C1`,看板"多机对比"页 |
| 测量系统分析 | Gage R&R 均值极差法(10零件×3评价人×3次),EV/AV/GRR/PV/ndc 及可接受性判定 |
| 失控模式注入与评估 | 生成器在预定时刻注入均值漂移/方差变大/周期波动/不良率上升,起点记录为真值;"仿真评估"页给出检出率、平均检出延迟、误报率(每 100 稳定子组) |

## 架构

```
cmd/spc            入口
internal/model     数据模型
internal/spc       纯计算: 控制图常数表、控制限、八准则、能力指数、Gage R&R (有单元测试)
internal/core      引擎: 接收过滤 → 子组化 → 控制限(重)算 → 判异 → 报警闭环
internal/generator 数据生成器: 8 台机 × 4 特性, 注入失控模式与录入错误, 统计及时性/误报率
internal/server    HTTP API + 内嵌看板(单页, Canvas 绘图, 无前端构建)
```

## API 摘要

- `GET /api/overview` 机台状态总览
- `GET /api/chart?char=C1&machine=M-01&last=120` 控制图数据(描点/控制限/判异点)
- `GET /api/alarms?status=open` 报警列表
- `POST /api/alarms/action {id, action, by}` 填处理措施
- `POST /api/alarms/close {id}` 关闭(仅"措施已生效"状态允许)
- `POST /api/rules {charId, rules:[8]bool}` 准则开关
- `POST /api/recalc {charId, machineId, reason}` 换模/换料重算控制限
- `GET /api/capability?char=C1&by=batch` 过程能力
- `GET /api/compare?char=C1` 多机对比
- `GET /api/msa?char=C1` Gage R&R
- `GET /api/eval` 仿真评估(检出率/延迟/误报率)
- `GET /api/quality` 数据质量过滤计数
- `POST` 之外, 外部测量系统可调用 `Engine.Ingest/IngestP` 实时写入

## 预置仿真剧本(模拟时间 08:00 起)

| 时刻 | 事件 | 位置 |
|---|---|---|
| +4h | 均值漂移 +1.5σ | C1 关键尺寸 / M-03 |
| +6h | 方差 ×2.2 | C1 / M-05 |
| +8h | 周期波动(幅 1.2σ, 周期 30 子组) | C2 重量 / M-02 |
| +10h | 不良率 1%→4% | C4 / M-04 |
| +12h | 均值漂移 -1.2σ | C3 浇口高度 / M-07 |
| +14h | 换模, 过程搬迁, 自动触发控制限重算(20子组后生效) | C1 / M-03 |

同时持续注入约 1% 缺失、0.5% 单位错误、0.3% 明显录入错误,验证数据过滤。

## 测试

```bash
go test ./...    # 八准则逐条正/反例、控制限数值、能力指数、Gage R&R
```
