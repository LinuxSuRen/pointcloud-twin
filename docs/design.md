# 技术方案（Design）

> 状态：v0.2。v0.1 草案经评审后进入实现，本文档随实现更新：
> 切片引擎由「嵌入 gocesiumtiler v3」调整为「自研纯 Go 生成器」（原因见 2.2 节决策记录），
> License 相应由 AGPL-3.0 调整为 MIT，CLI 名称定为 `pc2twin`。

## 1. 总体架构：双图层

纯点云 3D Tiles 的问题在于点云是"死"的：每个点没有设备语义，难以把"1 号电池舱的 SOC"精确绑定到特定点集上；即便 3D Tiles 1.1 支持 per-point 属性，在百万级点上做动态状态更新也不现实。因此采用双图层架构：

**图层一：点云 3D Tiles（静态背景层）**

将储能站现场扫描的 PCD 点云转换为 3D Tiles 1.1（GLB/glTF 瓦片）。这一层负责还原站区的真实空间结构——集装箱排列、设备布局、通道关系，提供厘米级的空间参照。它**不承载动态状态**，只做背景和环境参照。

**图层二：glTF 代理模型 + 实时数据（动态状态层）**

针对需要展示状态的设备（电池舱、PCS 柜、变压器等），从点云量取位置和包围盒尺寸，生成轻量的代理几何体（前端以半透明盒体渲染）。每个代理体赋予唯一的设备 ID，前端通过 WebSocket 接收 SCADA/BMS/PCS 的实时数据，根据设备 ID 将状态映射到代理体的颜色、透明度或悬浮标签上。

两者在 Cesium 场景中叠加渲染：点云提供真实感，代理模型提供可交互、可着色的状态语义。

## 2. Go 转换工具链

核心职责是把 PCD 变成 Cesium 可加载的 3D Tiles。流水线：

```text
PCD 输入 → （可选）体素降采样 → LAS 1.4 桥接文件（含 CRS）
                                 ↘ 自研切片引擎 → 3D Tiles 输出目录（tileset.json + tiles/）
                                 ↘ 设备代理体配置（devices.json：deviceId、中心、尺寸、朝向）
```

### 2.1 步骤一：PCD 解析与 LAS 桥接

- **PCD 解析自研**（`internal/pcd`）：覆盖 `ascii`、`binary`、`binary_compressed`（LZF 解压）三种编码；字段至少解析 x/y/z（float32/float64/整型），可选解析 `rgb/rgba`（打包或分离 r/g/b）与 `intensity`；NaN/Inf 点自动剔除。PCD 格式规范公开、二进制布局简单，自研解析器可控性最高。
- **LAS 1.4 写入自研**（`internal/las`）：点记录格式 7（XYZ + 强度 + GPS 时间 + RGB），WKT2 工程 CRS 写入 VLR 2112（全局编码 bit4 置位），另写自定义 VLR 以 JSON 记录 ENU 原点供本工具链回读。LAS 是与 PCL、PDAL、gocesiumtiler 等生态互通的桥接格式。
- **坐标系约定**（`internal/crs`、`internal/geo`）：源坐标一律按局部 ENU（东北天，米）解释；`--crs-enu "lon,lat,h"` 指定 WGS84 大地原点，缺省按 (0,0,0) 局部假设处理并如实标注。ENU→ECEF 用闭式公式实现，不依赖 PROJ。

### 2.2 步骤二：点云 → 3D Tiles（自研纯 Go 生成器）

**决策记录**：v0.1 草案原计划以库方式嵌入 gocesiumtiler v3。实现前调研发现其 v3 需要 **CGo + PROJ 9.8.1 静态库**，与本项目「单二进制、可交叉编译、无外部运行时依赖」的目标冲突（缺少 PROJ 构建环境时无法编译验证），经维护者确认改为自研，gocesiumtiler 降级为**可选外部工具路径**：本工具产出的 LAS 可直接交给 gocesiumtiler CLI 切片。License 相应从 AGPL-3.0 调整为 MIT。

自研引擎（`internal/tiler`）要点：

- **空间结构**：八叉树递归切分，直到单瓦片点数 ≤ `--points-per-tile`；
- **refine=add**（默认）：内部节点承载等步长抽稀的粗层，子节点只承载剩余点——每个点恰好落在一个瓦片内容中，不跨 LOD 重复存储；**refine=replace**：子节点承载全量点，父瓦片替换式细化；
- **3D Tiles 1.1**（默认）：瓦片为二进制 glTF（POINTS 图元，POSITION float32 + COLOR_0 uint8 normalized）。顶点保持 z-up 原值，glTF 根节点按规范写入 z-up→y-up 矩阵，由运行时的 y-up→z-up 旋转抵消；每瓦片坐标相对自身中心存储（float32 精度友好）；
- **3D Tiles 1.0**（`--tiles-version 1.0`）：瓦片为传统 `.pnts`（POSITION + RGB，8 字节对齐），兼容旧查看器；
- **地理放置**：tileset 根节点 `transform` = ENU→ECEF 变换（闭式公式）× 平移到点云包围盒中心，子瓦片 transform 相对父节点；
- **几何误差**：叶节点为 0，内部节点取子树包围盒对角线。

首版未做 meshopt 压缩与量化（gocesiumtiler v3 声称可再省约 75% 体积），列入后续版本。

### 2.3 CLI（`pc2twin`）

CLI 名称与仓库名解耦，定为 `pc2twin`（point cloud to twin）。子命令：

```bash
# 完整转换
pc2twin convert --input site.pcd --output ./out \
  --crs-enu "121.47,31.23,10" \
  --voxel 0.05 --points-per-tile 80000 --refine add \
  --tiles-version 1.1 --devices devices.json

# 只做格式桥接
pc2twin pcd2las --input site.pcd --output site.las
```

参数与错误码已冻结为文档：[cli.md](cli.md)。转换过程输出结构化分级日志（stdout，slog），关键链路出入口有 info 日志；用户可见错误为「稳定错误码 + 通俗提示」，技术根因只在 debug 日志。

### 2.4 转换报告

输出目录中的 `report.json` 记录输入点数、降采样结果、剔除点数、包围盒、CRS（类型 + ENU 原点 + 是否显式）、瓦片统计与关键参数——作为 R2 验收依据与「坐标系配错难察觉」风险的叠加校验手段。

### 2.5 服务化封装（`pc2twin serve`，对应需求 S3 首版）

同一个二进制通过 `serve` 子命令提供 Web 服务，支撑查看器「导入 PCD」入口：

- **分片续传上传**：init 稀疏预分配 + 分片 `WriteAt` 直写（峰值磁盘 1×，无拼接步骤）；`LimitReader(expected+1)` 边界校验防请求粘连；乱序/断点按 `receivedChunks` 对账，幂等重传；complete 可选 SHA-256 流式校验；会话 2 小时未活动过期清理。
- **异步任务**：`POST /api/jobs` → 202 + jobId；类型级并发上限 1（超限 409「已有转换任务在运行」）；`recover` 隔离 panic；cancelled 与 failed 严格区分（ctx 在阶段边界生效）；任务记录 10 分钟、产物目录 24 小时过期清理。
- **SSE 进度流**：三件套响应头（含 `X-Accel-Buffering: no`）、连接即推快照、500ms 轮询变化才写帧、终态即关；`GET /api/jobs/{id}` 为同源 REST 兜底。
- **进度钩子**：pipeline 暴露阶段权重回调（读取 30 / 降采样 10 / LAS 15 / 切片 45），切片阶段按瓦片粒度上报。
- **产物托管**：`GET /api/jobs/{id}/out/*` 直接供查看器加载；前端完成后销毁旧底图与实体并切换场景。
- **前端引擎**：并发窗口 3（夹紧 1~6）+ 单片重试 3 次（重试前先查 status 对账）；会话 404 自动重新 initiate 并经回调持久化（刷新页面可续传）；大文件流式 SHA-256（自实现增量实现，内存恒定）；进度以服务端 received_chunks 口径回调。

### 2.6 导出到其他平台（internal/export）

- **zip 打包**：`GET /api/jobs/{id}/export` 把任务产物目录（3D Tiles + devices.json + report.json）递归打包为 zip，部署到 CesiumJS / Cesium for UE / Unity 等 Cesium 系平台。
- **单文件 GLB**：`pc2twin export` CLI 与 `GET /api/jobs/{id}/export/glb` 现场生成——根节点携带 z-up→y-up 矩阵（本地 ENU 坐标、米制），子节点为点云（POINTS 图元 + 顶点色）与每台设备的盒体网格（TRS 对应 center/heading/size，材质按 deviceType 着色）；可导入 UE/Unity/Blender/three.js 等通用引擎。serve 导出设 800 万点上限（体积保护，超限提示导入时降采样）。
- **前端下载**：查看器在场景来自导入任务时显示导出按钮，使用原生导航下载（attachment 响应不离开页面，避免 fetch→blob 整文件驻留内存）。

### 2.7 设备图形化标注（对应需求 S2）

- **两点画盒**：查看器「标注设备」模式，点击点云表面两角点（`scene.pickPosition` 拾取 + ENU 逆变换回局部坐标）生成代理体；底面取 XY 外接矩形、盒底取较低 Z、默认高 3 米（最小边长 0.5 米防误点），几何逻辑独立于 Cesium 便于单测。
- **表单编辑**：选中设备后在面板编辑 deviceId/deviceType/center/size/heading，change 即应用（实体按设备整体重建，含面板纹理）；ID 冲突与非法尺寸在前端拦截并提示。
- **保存回写**：`PUT /api/jobs/{id}/out/devices.json` 校验（proxy.Validate）后写回任务产物，origin 缺省保留原值；非任务场景（如 mock 演示）不支持保存并有明确提示。
- 保存后的 devices.json 随 zip/GLB 导出进入交付物，形成「导入 → 转换 → 标注 → 导出」闭环。

## 3. 前端示例查看器（Vue 3）

- **技术栈**：Vue 3 + Vite + CesiumJS；构建产物为静态站点，可直接对接任意静态托管。
- **加载底图**：`Cesium3DTileset.fromUrl` 加载 `tileset.json`（查看器不依赖 Cesium ion，离线可用）。
- **渲染代理体**：读取 `devices.json`，按 `origin`（convert 注入的 ENU 原点）构造 ENU→ECEF 变换，为每台设备生成盒体实体：**盒面贴 Canvas 动态数据面板**（SOC/温度/功率/告警仪表盘，WebSocket 数据到达后按 1 秒节流批量重绘），描边随状态变色；建立 `deviceId → 实体` 映射表；点击或侧栏选中可查看详情。
- **状态数据通路**：WebSocket 接收数据帧，前端查映射表更新材质颜色与标签。告警等级转红、注意/低电量转黄、正常为绿；状态文案由前端映射表渲染，未知枚举走兜底文案。
- **为什么不在点云上直接承载状态**：3D Tiles 1.1 的 `EXT_structural_metadata` 与 Batch Table 确实支持要素级元数据，但对设备粒度的状态展示，代理体方案在更新性能、交互响应与视觉清晰度上远优于百万级点云逐点着色。

### WebSocket 消息契约（已冻结）

```json
{
  "type": "device-status",
  "ts": 1730000000000,
  "data": {
    "battery-cluster-01": { "soc": 0.82, "maxTemp": 31.5, "power": 120, "alarmLevel": 0, "ts": 1730000000000 }
  }
}
```

- `type`：`snapshot`（连接建立后全量快照）/ `device-status`（增量状态帧）/ `heartbeat`（心跳）；
- `alarmLevel` 等为稳定枚举，文案由前端映射表渲染，未知枚举走兜底文案；
- 客户端机制：指数退避自动重连（2s 起、上限 30s）、30 秒心跳看门狗、组件卸载自动断开；
- 参考实现：`web/mock/server.mjs`（同时静态托管转换产物，用于本地演示）。

## 4. 项目结构（实现）

```text
pointcloud-twin/
├── cmd/pc2twin/        # CLI 入口（convert / pcd2las / version）
├── internal/
│   ├── pcd/            # PCD 读取（ascii / binary / binary_compressed）
│   ├── pcdfix/         # PCD 测试夹具与示例数据生成
│   ├── las/            # LAS 1.4 写入
│   ├── tiler/          # 3D Tiles 生成（八叉树 / glb / pnts / tileset）
│   ├── downsample/     # 体素降采样
│   ├── proxy/          # 代理体配置读写与校验
│   ├── crs/            # ENU 原点定义与解析
│   ├── geo/            # WGS84 坐标换算
│   ├── pipeline/       # 流水线编排与转换报告
│   ├── apperr/         # 稳定错误码
│   └── logx/           # slog 日志构造
├── web/                # Vue 3 示例查看器 + 模拟数据源（mock/server.mjs）
├── examples/           # 示例点云与设备配置
├── docs/               # 需求、方案与 CLI 文档
└── .github/workflows/  # CI / CodeQL / Release（goreleaser 多平台发布）
```

## 5. 风险与开放问题

| 风险/问题 | 说明 | 缓解 |
|---|---|---|
| 大文件内存 | 亿级点云全量驻留内存有风险 | 首版限定千万点级（PCD binary 全量读入 + 点切片）；流水线化分块处理列入后续版本 |
| 自研切片引擎兼容性 | 需持续跟随 3D Tiles 规范演进 | 按 OGC 3D Tiles 1.1 规范实现并以 CesiumJS 为验收基准；单元测试覆盖 GLB/pnts 二进制布局 |
| 瓦片体积 | 首版无 meshopt 压缩/量化 | 后续版本引入 `KHR_mesh_quantization` + `EXT_meshopt_compression` |
| 坐标系错误难察觉 | CRS 配错会导致瓦片位置漂移 | 输出转换报告（点数、包围盒、原点），查看器叠加校验 |
| gocesiumtiler 外部路径 | 用户自带 gocesiumtiler 处理 LAS 时需自行处理其 CRS 参数 | 文档说明其 `--crs` 用法与本项目 LAS 的 ENU 语义 |

## 6. 与需求的对齐

本方案覆盖 [requirements.md](requirements.md) 的 R1–R5：R1 由 `internal/pcd` + `internal/tiler` 落地（3D Tiles 1.1 默认、1.0 可选、降采样可选）；R2 由 `internal/crs`/`internal/geo` + LAS 双 VLR + tileset 根变换 + 转换报告落地；R3 由 `internal/proxy` 与 `--devices` 流程落地；R4 由 cobra CLI + slog + `apperr` 错误码落地（文档见 [cli.md](cli.md)）；R5 由 `web/` 查看器与 mock 数据源落地。S1 部分落地（体素降采样）；S3 已落地首版（`pc2twin serve`：分片上传 + 异步任务 + SSE 进度 + 产物托管，见 2.5 节）；S2（图形化标注）、S4（批量多站点）不在首版范围。
