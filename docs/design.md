# 技术方案（Design）

> 状态：v0.1 草案，等待评审。本文档描述 pointcloud-twin 怎么做，是需求的实现方案基线。

## 1. 总体架构：双图层

纯点云 3D Tiles 的问题在于点云是"死"的：每个点没有设备语义，难以把"1 号电池舱的 SOC"精确绑定到特定点集上；即便 3D Tiles 1.1 支持 per-point 属性，在百万级点上做动态状态更新也不现实。因此采用双图层架构：

**图层一：点云 3D Tiles（静态背景层）**

将储能站现场扫描的 PCD 点云转换为 3D Tiles 1.1（GLB/glTF 瓦片）。这一层负责还原站区的真实空间结构——集装箱排列、设备布局、通道关系，提供厘米级的空间参照。它**不承载动态状态**，只做背景和环境参照。

**图层二：glTF 代理模型 + 实时数据（动态状态层）**

针对需要展示状态的设备（电池舱、PCS 柜、变压器等），从点云中量取位置和包围盒尺寸，生成轻量的 glTF 代理几何体（半透明线框或简化立方体）。每个代理体赋予唯一的设备 ID，前端通过 WebSocket 接收 SCADA/BMS/PCS 的实时数据，根据设备 ID 将状态映射到代理体的颜色、透明度或悬浮标签上。

两者在 Cesium 场景中叠加渲染：点云提供真实感，代理模型提供可交互、可着色的状态语义。

## 2. Go 转换工具链

核心职责是把 PCD 变成 Cesium 可加载的 3D Tiles。`gocesiumtiler` 是 Go 生态中较成熟的开源 3D Tiles 点云生成器，但不直接读 PCD（输入是 LAS/LAZ/E57），因此分两步，以 LAS 为桥接格式。

### 2.1 步骤一：PCD → LAS

**方案 A：纯 Go 实现（首版选型）**

- 使用 Go 的 PCD 读取能力（候选库：`seqsense/pcgol` 的 PCD header 解析，`dereklison/pcd` 的 encoder/decoder）读取 XYZ 坐标（及可选 intensity/rgb）。
- 使用 Go 的 LAS 写入库（候选：`softlandia/glasio` 或 `lidario`）写出标准 LAS 1.4 文件。
- **保留坐标系信息**：现场扫描导出的 PCD 通常没有地理参考；工具允许显式指定局部坐标系（例如以站区某点为原点的 ENU），并把 CRS 写入 LAS 或在切片时通过 `--crs` 传入。

选型理由：无外部运行时依赖、可控性最高、便于交叉编译发布单二进制。实现时若发现成熟库对 binary PCD 支持不足，自研解析器兜底（PCD 格式规范公开，binary 布局简单）。

**方案 B：封装 PDAL（备选，降采样需求变重时启用）**

- 通过 `os/exec` 封装 `pdal translate input.pcd output.las`，格式转换交给 PDAL。
- 优点是省事且 pipeline 能力强（滤波、降采样）；缺点是引入外部依赖，违背单二进制分发目标，故仅作备选。

### 2.2 步骤二：LAS → 3D Tiles（嵌入 gocesiumtiler 库）

`gocesiumtiler` v3 提供 Library-First Go API，直接在代码中构造 tiler 实例（输入 LAS 路径、输出目录、CRS、每瓦片点数等），不走命令行。

对储能站场景关键的参数：

- **`--version 1.1`**：输出 3D Tiles 1.1（GLB/glTF 瓦片），兼容性好，便于与代理模型统一渲染栈。
- **`--points-per-tile`**：站区级场景 5 万～10 万点/瓦片为合理起点。
- **`--refine-mode add`**：ADD 模式避免跨 LOD 重复存储点，对网络带宽友好。
- **压缩**：v3 默认启用 `EXT_meshopt_compression` 与 `KHR_mesh_quantization`，瓦片体积可压缩约 75%，对 Web 传输重要。

> **许可证提醒**：`gocesiumtiler` v3 为 **AGPLv3**。本项目以库方式嵌入，故整体采用 **AGPL-3.0**；如未来需要闭源商业分发，需联系其作者获取商业许可。

### 2.3 流水线

```text
PCD 输入 → （可选）预处理/降采样 → LAS 中间文件（含 CRS） → gocesiumtiler 库调用 → 3D Tiles 输出目录（tileset.json + 瓦片）
                                      ↘ 设备代理体配置（JSON/YAML：deviceId、中心坐标、尺寸、朝向）
```

Go 工具同时完成"几何底图"和"状态挂载点"的双重准备：

- 3D Tiles 输出目录；
- 设备代理体配置（首版人工标注/手写，见 R3）。

### 2.4 CLI 设计（草案）

```bash
# 完整转换
pc2twin convert --input site.pcd --output ./out \
  --crs-enu "origin=121.47,31.23,10" \
  --voxel 0.05 --points-per-tile 80000 --refine add

# 只做格式桥接
pc2twin pcd2las --input site.pcd --output site.las
```

约定：CLI 名与仓库名解耦，最终名称实现前敲定；参数与错误码在实现阶段冻结为文档。

## 3. 前端示例查看器（Vue 3）

- **技术栈**：Vue 3 + Vite + CesiumJS（3D Tiles 支持最完整）；构建产物为静态站点，可直接对接任意静态托管。
- **加载底图**：CesiumJS `Cesium3DTileset` 加载转换产出的 `tileset.json`。
- **渲染代理体**：读取代理体配置，为每台设备生成半透明 glTF 盒体（Cesium Entity / Model），建立 `deviceId → 代理体对象` 映射表。
- **状态数据通路**：后端（或示例中的模拟源）经 WebSocket 推送 `{deviceId, metrics: {soc, temp, power, alarmLevel}, ts}` 数据帧；前端查映射表更新材质颜色/透明度/悬浮标签。例如：SOC 低于阈值变橙色，温度异常闪烁红色，正常半透明绿色。
- **为什么不在点云上直接承载状态**：3D Tiles 1.1 的 `EXT_structural_metadata` 与 Batch Table 确实支持要素级元数据，但对设备粒度的状态展示，代理体方案在更新性能、交互响应与视觉清晰度上远优于百万级点云逐点着色。

### WebSocket 消息契约（草案）

```json
{
  "type": "device-status",
  "ts": 1730000000000,
  "data": {
    "battery-cluster-01": { "soc": 0.82, "maxTemp": 31.5, "alarmLevel": 0 }
  }
}
```

- `type` 区分消息类型（状态帧 / 全量快照 / 心跳）；`alarmLevel` 等为稳定枚举，文案由前端映射表渲染，未知枚举走兜底文案。
- 配套机制：断线重连、心跳、服务端顺序保证在实现阶段细化。

## 4. 项目结构（规划）

```text
pointcloud-twin/
├── cmd/pc2twin/        # Go CLI 入口
├── internal/
│   ├── pcd/            # PCD 读取（ASCII/binary）
│   ├── las/            # LAS 写入
│   ├── pipeline/       # 流水线编排（convert / pcd2las 子命令）
│   └── proxy/          # 代理体配置读写与校验
├── web/                # Vue 3 + Vite 示例查看器
├── docs/               # 需求与方案文档
└── .github/workflows/  # CI / Lint / Release / CodeQL
```

## 5. 风险与开放问题

| 风险/问题 | 说明 | 缓解 |
|---|---|---|
| Go PCD 库成熟度 | `pcgol`/`dereklison/pcd` 对 binary PCD 支持需验证 | 实现首周做 spike 验证；不行则自研解析器（格式规范简单） |
| gocesiumtiler AGPL | 闭源商用需商业许可 | 项目采用 AGPL-3.0；商用路径在 README 注明 |
| 大文件内存 | 亿级点云全量驻留内存有风险 | 首版限定千万点级；流水线化分块处理列入后续版本 |
| 坐标系错误难察觉 | CRS 配错会导致瓦片位置漂移 | 输出转换报告（点数、包围盒、原点），查看器叠加校验 |

## 6. 与需求的对齐

本方案覆盖 [requirements.md](requirements.md) 的 R1–R5；S1/S2 对应 PDAL 备选路径与查看器内标注工具；第 5 节"明确不做"项均不在方案范围内。
