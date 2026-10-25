# pointcloud-twin

[English introduction](#english-introduction)

**pointcloud-twin** 是一个把**点云（PCD）转换为数字孪生可用模型**的开源工具链，技术栈为 **Golang + Vue**。

它面向储能站、工厂、园区等实物场景的数字孪生建设：现场三维扫描得到 PCD 点云，工具链将其转换为 Web 端可高效加载的 **3D Tiles 1.1** 底图，并产出**设备代理模型配置**（设备包围盒 + 设备 ID），供前端把 SCADA/BMS/PCS 等实时状态映射到对应设备上。

## 核心思路：点云作底图，代理模型承载状态

纯点云 3D Tiles 难以承载设备粒度的动态状态——每个点没有设备语义，在百万级点上做动态状态更新也不现实。因此本项目采用**双图层架构**：

1. **点云 3D Tiles（静态背景层）**：真实还原站区空间结构（集装箱排列、设备布局、通道关系），提供厘米级空间参照，不承载动态状态。
2. **glTF 代理模型 + 实时数据（动态状态层）**：对需要展示状态的设备（电池舱、PCS 柜、变压器等），从点云量取位置和包围盒，生成轻量代理几何体；前端经 WebSocket 接收实时数据，按设备 ID 将状态映射到代理体的颜色、透明度或悬浮标签。

两层在 Cesium 场景中叠加渲染：点云负责"真实"，代理体负责"语义"。

## 快速开始

### 1. 安装与构建

从源码构建（需要 Go 1.26+）：

```bash
git clone https://github.com/LinuxSuRen/pointcloud-twin.git
cd pointcloud-twin
go build -o bin/pc2twin ./cmd/pc2twin
```

各平台二进制可从 [Releases](https://github.com/LinuxSuRen/pointcloud-twin/releases) 获取（打 tag 后由 CI 自动发布）。

### 2. 转换点云

```bash
# 用仓库自带示例数据体验（含模拟储能站点云与 6 台设备配置）
./bin/pc2twin convert -i examples/site.sample.pcd -o out \
  --crs-enu "121.47,31.23,10" \
  --devices examples/devices.example.json
```

输出目录包含 `tileset.json` + `tiles/`（3D Tiles 1.1，glb 瓦片）、`devices.json`（代理体配置）、`site.sample.pcd.las`（LAS 1.4 桥接文件）与 `report.json`（转换报告）。

常用参数（完整参考见 [docs/cli.md](docs/cli.md)）：

| 参数 | 说明 |
|---|---|
| `--crs-enu "lon,lat,h"` | 局部 ENU 原点（度，米），决定瓦片落地的地理位置 |
| `--voxel 0.05` | 体素降采样边长（米） |
| `--points-per-tile 80000` | 单瓦片目标点数 |
| `--refine add` | LOD 模式：add / replace |
| `--tiles-version 1.1` | 3D Tiles 版本：1.1（glb）/ 1.0（pnts） |
| `--devices devices.json` | 设备代理体配置，校验后拷入输出目录 |

### 3. 启动示例查看器

```bash
cd web
pnpm install
pnpm mock &    # 模拟数据源：静态托管 out/ + WebSocket 推送设备状态
pnpm dev       # 打开 http://localhost:5173
```

查看器（Vue 3 + Vite + CesiumJS）会加载点云底图、渲染 6 台设备的半透明代理体，并用模拟数据实时驱动代理体颜色与状态标签：温度过高转红、低电量转黄、正常为绿；点击代理体或侧栏设备可查看详情。

## 仓库结构

```text
pointcloud-twin/
├── cmd/pc2twin/        # Go CLI 入口（convert / pcd2las / version）
├── internal/
│   ├── pcd/            # PCD 读取（ascii / binary / binary_compressed）
│   ├── las/            # LAS 1.4 写入（PDR7 + WKT CRS + ENU 原点 VLR）
│   ├── tiler/          # 3D Tiles 生成（八叉树 + glb/pnts 编码 + ENU→ECEF）
│   ├── downsample/     # 体素降采样
│   ├── proxy/          # 设备代理体配置读写与校验
│   ├── crs/            # ENU 原点定义与解析
│   ├── geo/            # WGS84 坐标换算（闭式公式，无 PROJ 依赖）
│   ├── pipeline/       # 流水线编排与转换报告
│   ├── apperr/         # 稳定错误码
│   ├── logx/           # slog 日志
│   └── pcdfix/         # PCD 测试夹具生成
├── web/                # Vue 3 示例查看器 + 模拟数据源
├── examples/           # 示例点云与设备配置
├── docs/               # 需求、方案与 CLI 文档
└── .github/workflows/  # CI / CodeQL / Release
```

## 实时数据契约

查看器通过 WebSocket 接收数据帧（详见 [docs/design.md](docs/design.md)）：

```json
{
  "type": "device-status",
  "ts": 1730000000000,
  "data": {
    "battery-cluster-01": { "soc": 0.82, "maxTemp": 31.5, "power": 120, "alarmLevel": 0, "ts": 1730000000000 }
  }
}
```

`type` 为 `snapshot`（连接后全量快照）、`device-status`（增量状态帧）或 `heartbeat`（心跳）；`alarmLevel` 为稳定枚举（0 正常 / 1 注意 / 2 警告 / 3 严重），展示文案由前端映射表渲染，未知枚举有兜底。本项目不内置 SCADA/BMS/PCS 采集网关，实时数据接入以本契约为边界（`web/mock/server.mjs` 是参考实现）。

## 路线图

- [x] 阶段一：PCD → LAS 转换（含 CRS 处理与可选降采样）
- [x] 阶段二：点云 → 3D Tiles 1.1（自研纯 Go 切片引擎，选型说明见 [docs/design.md](docs/design.md)）
- [x] 阶段三：设备代理体配置输出（人工标注/手写配置 + 校验）
- [x] 阶段四：Vue 3 + Cesium 示例查看器（加载 3D Tiles、渲染代理体、WebSocket 状态驱动）
- [x] 阶段五：CLI 打磨、CI/CD、发布

## 技术栈

- 后端 / 转换工具：Go（PCD 读取、LAS 写入、3D Tiles 生成，纯标准库实现 + cobra CLI，可交叉编译单二进制）
- 前端示例：Vue 3 + Vite + CesiumJS

## 贡献

欢迎通过 Issue 与 Pull Request 参与，提交前请阅读 [docs/requirements.md](docs/requirements.md) 与 [CONTRIBUTING.md](CONTRIBUTING.md)。

## License

本项目基于 [MIT](LICENSE) 开源。

---

## English introduction

**pointcloud-twin** is an open-source toolchain (Golang + Vue) that converts point clouds (PCD) into assets for digital twins: it turns scanned PCD data into **3D Tiles 1.1** background layers for the Web, and emits **device proxy-model configs** (bounding boxes + device IDs) so real-time data from SCADA/BMS/PCS systems can be mapped onto devices in a Cesium-based viewer. The conversion tool (`pc2twin`) is a single static binary; see [docs/cli.md](docs/cli.md) for the CLI reference and [docs/design.md](docs/design.md) for the architecture.
