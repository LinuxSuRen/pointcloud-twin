# pc2twin CLI 参考

`pc2twin` 是 pointcloud-twin 的命令行工具，把 PCD 点云转换为数字孪生可用资产：3D Tiles 底图 + 设备代理体配置。

## 命令

### `pc2twin convert`

完整转换：PCD →（可选降采样）→ LAS 桥接文件 + 3D Tiles 目录 + 代理体配置 + 转换报告。

```bash
pc2twin convert --input site.pcd --output ./out \
  --crs-enu "121.47,31.23,10" \
  --voxel 0.05 \
  --points-per-tile 80000 \
  --refine add \
  --tiles-version 1.1 \
  --devices devices.json
```

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--input, -i` | 必填 | 输入 PCD 文件路径 |
| `--output, -o` | 必填 | 输出目录 |
| `--crs-enu` | 空（局部假设） | 局部 ENU 原点，格式 `lon,lat,h`（度，米；h 可省略） |
| `--voxel` | 0（不降采样） | 体素降采样边长（米） |
| `--points-per-tile` | 50000 | 单瓦片目标点数 |
| `--refine` | add | LOD 细化模式：`add` 点不跨层重复存储；`replace` 子瓦片承载全量点 |
| `--tiles-version` | 1.1 | 3D Tiles 版本：`1.1`（glb 瓦片）或 `1.0`（pnts 瓦片） |
| `--devices` | 空 | 设备代理体配置（JSON），校验通过后拷贝到输出目录并注入 ENU 原点 |
| `--skip-las` | false | 不写中间 LAS 文件 |
| `--log-level` | info | 日志级别：debug / info / warn / error |

输出目录结构：

```text
out/
├── tileset.json          # 3D Tiles 入口
├── tiles/                # 瓦片（.glb 或 .pnts；启用平面分割时含面片网格瓦片）
├── site.pcd.las          # LAS 1.4 桥接文件（含 CRS，--skip-las 关闭）
├── planes.json           # 平面分割结果（启用 --planes 时输出，供导出复用）
├── devices.json          # 代理体配置（提供 --devices 时输出，带 ENU 原点）
└── report.json           # 转换报告（点数、包围盒、CRS、参数、平面覆盖率）
```

## 平面拟合分割（--planes）

站区类场景（地面、集装箱面、墙面）以平面为主，启用后大平面被重建为**面片网格瓦片**（按内点平均色着色、双面材质），残余点仍以点云瓦片呈现——观感更实体、数据量更小、平面可量测。

```bash
pc2twin convert -i site.pcd -o out --planes "dist=0.05,min=500"
```

参数（k=v 逗号串，均可省略）：

| 键 | 默认 | 说明 |
|---|---|---|
| `dist` | 0.03 | 内点距离阈值（米） |
| `min` | 500 | 单平面最小内点数 |
| `max` | 0（不限） | 最多抽取平面数 |
| `grid` | 0.1 | 边界栅格分辨率（米，实际会按采样密度自适应放大） |
| `tol` | 0.2 | 边界多边形简化容差（米） |
| `trials` | 128 | 每平面 RANSAC 采样次数 |
| `seed` | 1 | 随机种子（固定保证结果可复现） |

实现要点：迭代 RANSAC（邻域采样提升命中共面三点）→ 占据栅格 + 连通域 + 裂缝跟踪提取边界多边形 → DP 简化 → 耳切三角化；法向符号归一（最大分量取正）；剔除小于最大连通片面积 10% 的碎屑。结果写入 `planes.json`（含 dist 与多边形顶点），导出 GLB/zip 时复用；覆盖率写入 `report.json` 的 `planes` 段。仅在 `--tiles-version 1.1` 下生成面片瓦片。

### `pc2twin pcd2las`

只做格式桥接：PCD → LAS 1.4（点记录格式 7，含 WKT CRS 与 ENU 原点 VLR）。

```bash
pc2twin pcd2las --input site.pcd --output site.las --crs-enu "121.47,31.23,10"
```

| 参数 | 说明 |
|---|---|
| `--input, -i` | 输入 PCD 文件路径（必填） |
| `--output, -o` | 输出 LAS 文件路径（必填） |
| `--crs-enu` | 同 convert |
| `--voxel` | 同 convert |
| `--log-level` | 同 convert |

### `pc2twin serve`

启动 Web 服务：网页内导入 PCD（分片续传上传）、异步转换（SSE 进度流）、托管转换产物供查看器直接加载。

```bash
pc2twin serve --addr 127.0.0.1:8080 --data ./pc2twin-data
```

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--addr, -a` | 127.0.0.1:8080 | 监听地址 |
| `--data` | pc2twin-data | 数据目录（上传暂存 + 任务产物） |
| `--chunk-size` | 16 | 分片上传单片大小（MB） |
| `--max-upload` | 2048 | 单文件上传上限（MB） |
| `--log-level` | info | 日志级别 |

**任务 API**：

| 接口 | 说明 |
|---|---|
| `POST /api/uploads/init` | `{filename, size, purpose: "pcd"\|"devices"}` → `202 {uploadId, chunkSize}` |
| `PUT /api/uploads/{id}/chunks/{index}` | raw body 直写单片（末片按剩余字节计），幂等可重传 |
| `GET /api/uploads/{id}` | 查询分片对账（`receivedChunks`），断点续传依据 |
| `POST /api/uploads/{id}/complete` | `{sha256}` 可选完整性校验，定稿 |
| `DELETE /api/uploads/{id}` | 放弃上传并清理暂存 |
| `POST /api/jobs` | `{pcdUploadId, devicesUploadId?, options{crsEnu, voxel, pointsPerTile, refine, tilesVersion}}` → `202 {jobId}`；已有任务在运行时返回 `409` |
| `GET /api/jobs/{id}` | 任务状态兜底查询（与 SSE 同源同结构） |
| `DELETE /api/jobs/{id}` | 取消任务（cancelled 与 failed 区分） |
| `GET /api/jobs/{id}/events` | SSE 进度流（`X-Accel-Buffering: no`、变化才推、终态即关） |
| `GET /api/jobs/{id}/out/*` | 托管转换产物（tileset.json / tiles / devices.json / report.json） |
| `GET /api/jobs/{id}/export` | 导出 zip（3D Tiles 目录 + 配置 + 报告） |
| `GET /api/jobs/{id}/export/glb` | 导出单文件 GLB（点云 + 设备盒体，800 万点上限） |
| `PUT /api/jobs/{id}/out/devices.json` | 查看器内标注/编辑后的设备配置写回（校验失败 400；origin 缺省时保留任务产物原值） |

**任务状态机**：`running → succeeded | failed | cancelled`；同一时刻仅允许 1 个转换任务（CPU/内存密集，串行更稳）；任务记录保留 10 分钟、产物目录保留 24 小时后自动清理；上传会话 2 小时未活动过期。

**服务侧错误码**（在转换错误码之外新增）：

| 错误码 | 含义 |
|---|---|
| E7001 | 请求参数无效 |
| E7002 | 暂存文件读写失败 |
| E7003 | 上传会话无效（不存在、未定稿、校验失败） |
| E7004 | 任务不存在或已过期 |
| E7005 | 当前连接不支持流式响应 |
| E7006 | 导出点数超限（默认 800 万点，请导入时降采样） |
| E9001 | 任务内部错误（服务端已隔离，详见日志） |
| E9002 | 转换失败（未归类错误） |

### `pc2twin export`

把点云与设备代理体组装为**单文件 GLB**（glTF 2.0 二进制）数字孪生模型，可导入 UE / Unity / Blender / three.js 等通用引擎。

```bash
pc2twin export --input site.pcd --devices devices.json --output twin.glb --voxel 0.05
```

| 参数 | 说明 |
|---|---|
| `--input, -i` | 输入 PCD 文件路径（必填） |
| `--devices` | 设备代理体配置（JSON，可选；提供时设备以彩色盒体网格导出） |
| `--output, -o` | 输出 GLB 文件路径（必填） |
| `--voxel` | 导出时体素降采样边长（米）；0 表示不降采样 |
| `--planes` | 平面拟合分割参数（同 convert），平面导出为实色面片网格 |
| `--log-level` | 日志级别 |

模型结构：根节点携带 z-up→y-up 矩阵（导入通用引擎后方向正确），子节点为 `pointcloud`（POINTS 图元，顶点色保留）与每台设备一个盒体节点（translation/rotation/scale 对应 center/heading/size，材质按 deviceType 着色）。

serve 侧对应接口：`GET /api/jobs/{id}/export`（zip 打包 3D Tiles 产物）与 `GET /api/jobs/{id}/export/glb`（现场生成 GLB），查看器在场景来自导入任务时显示「导出 zip / 导出 GLB」按钮。

### `pc2twin version`

显示版本信息。

## PCD 输入支持

- 编码：`ascii`、`binary`、`binary_compressed`（LZF）
- 字段：`x/y/z` 必需（float32/float64/整型均可）；`rgb`/`rgba`（打包）与分离的 `r/g/b`、`intensity` 可选
- 坐标为 NaN/Inf 的点自动剔除并计入日志与报告的 `skipped`

## 坐标系（CRS）约定

- 源坐标始终按**局部 ENU（东北天，米）**解释；`--crs-enu` 指定该坐标系的大地原点（WGS84）。
- 不指定 `--crs-enu` 时按原点 (0,0,0) 的局部假设处理，并在报告与 LAS 中如实标注 `explicit: false`。
- CRS 信息落两处：LAS 的 WKT VLR（record 2112）+ 自定义 VLR（JSON）；3D Tiles 根瓦片 `transform` 直接携带 ENU→ECEF 变换。

## 设备代理体配置（devices.json）

```json
{
  "origin": { "lon": 121.47, "lat": 31.23, "h": 10, "explicit": true },
  "devices": [
    {
      "deviceId": "battery-cluster-01",
      "deviceType": "battery-cluster",
      "center": [12, 5.3, 1.5],
      "size": [12, 2.6, 3],
      "heading": 0
    }
  ]
}
```

- `deviceId`：人类可读的稳定标识，是前端与实时数据流的映射键，必须唯一
- `center`/`size`：点云局部 ENU 坐标（米）
- `heading`：绕天向朝向角（度，0 = 东）
- `origin` 由 convert 自动注入，查看器据此把代理体与瓦片集对齐

## 错误码

用户可见错误由「稳定错误码 + 通俗提示」组成，技术根因只在 `--log-level debug` 日志中。

| 错误码 | 含义 |
|---|---|
| E1001 | 输入文件不存在或不可读 |
| E1002 | 无法解析 PCD 文件 |
| E1003 | PCD 字段缺少 x/y/z 坐标 |
| E1004 | 输入点云为空 |
| E2001 | LAS 文件写入失败 |
| E2002 | 输出目录创建失败或不可写 |
| E3001 | CRS / ENU 原点参数格式无效 |
| E4001 | 设备代理体配置解析失败 |
| E4002 | 设备代理体配置校验失败 |
| E5001 | 3D Tiles 生成失败 |
| E6001 | 参数取值不支持 |

## 退出码

- `0`：成功
- `1`：失败（stderr 输出 `错误：E1002：无法解析 PCD 文件（…）` 形式的信息）
