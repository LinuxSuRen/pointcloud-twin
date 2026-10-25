# 示例数据

| 文件 | 说明 |
|---|---|
| `site.sample.pcd` | 模拟储能站点云（binary 编码，13013 点，60m × 30m：地面 + 三排集装箱体块） |
| `devices.example.json` | 与点云匹配的 6 台设备代理体配置（5 个电池舱 + 1 个 PCS 柜） |

## 本地演示

```bash
# 1. 转换（在仓库根目录）
go run ./cmd/pc2twin convert -i examples/site.sample.pcd -o out \
  --crs-enu "121.47,31.23,10" --devices examples/devices.example.json

# 2. 启动模拟数据源（静态托管 out/ + WebSocket 推送设备状态）
cd web
pnpm install
pnpm mock -- --root ../out

# 3. 启动查看器（另开终端）
pnpm dev
# 打开 http://localhost:5173
```

设备坐标（`center`/`size`）与点云同处一个局部 ENU 坐标系（米），`--crs-enu` 给出该坐标系的大地原点；转换后 `out/devices.json` 会自动带上原点信息供查看器对齐。
