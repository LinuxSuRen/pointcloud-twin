# pointcloud-twin

[English introduction](#english-introduction)

**pointcloud-twin** 是一个把**点云（PCD）转换为数字孪生可用模型**的开源工具链，技术栈为 **Golang + Vue**。

它面向储能站、工厂、园区等实物场景的数字孪生建设：现场三维扫描得到 PCD 点云，工具链将其转换为 Web 端可高效加载的 **3D Tiles 1.1** 底图，并产出**设备代理模型配置**（设备包围盒 + 设备 ID），供前端把 SCADA/BMS/PCS 等实时状态映射到对应设备上。

## 核心思路：点云作底图，代理模型承载状态

纯点云 3D Tiles 难以承载设备粒度的动态状态——每个点没有设备语义，在百万级点上做动态状态更新也不现实。因此本项目采用**双图层架构**：

1. **点云 3D Tiles（静态背景层）**：真实还原站区空间结构（集装箱排列、设备布局、通道关系），提供厘米级空间参照，不承载动态状态。
2. **glTF 代理模型 + 实时数据（动态状态层）**：对需要展示状态的设备（电池舱、PCS 柜、变压器等），从点云量取位置和包围盒，生成轻量 glTF 代理几何体；前端经 WebSocket 接收实时数据，按设备 ID 将状态映射到代理体的颜色、透明度或悬浮标签。

两层在 Cesium 场景中叠加渲染：点云负责"真实"，代理体负责"语义"。

## 仓库当前状态

本仓库目前处于**需求与方案设计阶段**，仅包含需求文档与技术方案，尚未开始实现。实现计划见下面的路线图。

- 需求文档：[docs/requirements.md](docs/requirements.md)
- 技术方案：[docs/design.md](docs/design.md)

## 路线图

- [ ] 阶段一：PCD → LAS 转换（含 CRS 处理与可选降采样）
- [ ] 阶段二：LAS → 3D Tiles 1.1（嵌入 gocesiumtiler 库）
- [ ] 阶段三：设备代理体配置输出（人工标注/交互式量取包围盒）
- [ ] 阶段四：Vue 3 + Cesium 示例查看器（加载 3D Tiles、渲染代理体、WebSocket 状态驱动）
- [ ] 阶段五：CLI 打磨、CI/CD、发布

## 技术栈

- 后端 / 转换工具：Go（PCD 读取、LAS 写入、gocesiumtiler 库集成）
- 前端示例：Vue 3 + Vite + CesiumJS

## 贡献

欢迎通过 Issue 与 Pull Request 参与，提交前请阅读 [docs/requirements.md](docs/requirements.md) 了解当前范围。

## License

本项目基于 [AGPL-3.0](LICENSE) 开源（因嵌入 AGPLv3 的 gocesiumtiler 库）。

---

## English introduction

**pointcloud-twin** is an open-source toolchain (Golang + Vue) that converts point clouds (PCD) into assets for digital twins: it turns scanned PCD data into **3D Tiles 1.1** background layers for the Web, and emits **device proxy-model configs** (bounding boxes + device IDs) so real-time data from SCADA/BMS/PCS systems can be mapped onto devices in a Cesium-based viewer. The project is currently at the requirements & design stage; see [docs/requirements.md](docs/requirements.md) and [docs/design.md](docs/design.md) (in Chinese).
