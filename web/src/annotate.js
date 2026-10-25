// 设备标注的纯逻辑：两点角 → 设备几何、默认命名。便于单测与复用。

// minExtent 是画盒的最小边长（米），避免误点出薄片。
const minExtent = 0.5

// defaultHeight 是默认盒高（米）。
export const defaultHeight = 3

// deviceFromCorners 由两次点选的角点（局部 ENU，米）构造设备几何。
// 底面取两点在 XY 平面的外接矩形，盒底取两点中较低的 Z，高取 defaultHeight。
export function deviceFromCorners(c1, c2, height = defaultHeight) {
  const x1 = Math.min(c1[0], c2[0])
  const x2 = Math.max(c1[0], c2[0])
  const y1 = Math.min(c1[1], c2[1])
  const y2 = Math.max(c1[1], c2[1])
  const baseZ = Math.min(c1[2], c2[2])
  const dx = Math.max(x2 - x1, minExtent)
  const dy = Math.max(y2 - y1, minExtent)
  const dz = Math.max(height, minExtent)
  return {
    center: [(x1 + x2) / 2, (y1 + y2) / 2, baseZ + dz / 2],
    size: [dx, dy, dz],
  }
}

// uniqueDeviceId 生成不与现有 ID 冲突的默认设备 ID。
export function uniqueDeviceId(existing, prefix = 'device') {
  const used = new Set(existing)
  for (let n = 1; ; n++) {
    const id = `${prefix}-${String(n).padStart(2, '0')}`
    if (!used.has(id)) {
      return id
    }
  }
}
