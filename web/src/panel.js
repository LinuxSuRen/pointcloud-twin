// 设备数据面板：把实时指标绘制到离屏 Canvas，作为纹理贴在代理体表面。
// 绘制目标是一个鸭子类型的 2D 上下文（便于在 Node 环境测试）。
import { alarmText, formatPower, formatSoc, formatTemp, statusColor, STATUS_COLORS } from './mapping.js'

const THEME = {
  bg: '#171e27',
  border: '#2a3543',
  text: '#c8d2de',
  dim: '#8b99ab',
  mono: 'bold 40px "SFMono-Regular", Consolas, monospace',
  sansId: 'bold 34px "PingFang SC", "Microsoft YaHei", sans-serif',
  sansSmall: '28px "PingFang SC", "Microsoft YaHei", sans-serif',
}

// drawPanel 把一帧面板内容绘制到 ctx（宽高比 2:1，如 512×256）。
export function drawPanel(ctx, device, metrics, w = 512, h = 256) {
  const pad = 24
  const color = metrics ? statusColor(metrics) : STATUS_COLORS.unknown

  // 背景
  ctx.fillStyle = THEME.bg
  ctx.fillRect(0, 0, w, h)
  ctx.strokeStyle = THEME.border
  ctx.lineWidth = 4
  ctx.strokeRect(2, 2, w - 4, h - 4)

  // 标题行：设备 ID + 状态徽标
  ctx.fillStyle = THEME.text
  ctx.font = THEME.sansId
  ctx.textAlign = 'left'
  ctx.textBaseline = 'top'
  ctx.fillText(device.deviceId, pad, pad)

  const status = metrics ? alarmText(metrics.alarmLevel) : '等待数据'
  ctx.font = THEME.sansSmall
  ctx.textAlign = 'right'
  const badgeW = ctx.measureText(status).width + 36
  ctx.fillStyle = color
  ctx.beginPath()
  ctx.arc(w - pad - badgeW + 12, pad + 18, 12, 0, Math.PI * 2)
  ctx.fill()
  ctx.fillText(status, w - pad, pad)

  if (!metrics) {
    ctx.fillStyle = THEME.dim
    ctx.font = THEME.sansSmall
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    ctx.fillText('等待实时数据…', w / 2, h / 2 + 24)
    return
  }

  // SOC 条形图
  const soc = typeof metrics.soc === 'number' ? Math.max(0, Math.min(1, metrics.soc)) : null
  const barY = pad + 64
  const barW = w - pad * 2
  const barH = 34
  ctx.fillStyle = '#1e2733'
  ctx.fillRect(pad, barY, barW, barH)
  if (soc !== null) {
    ctx.fillStyle = color
    ctx.fillRect(pad, barY, barW * soc, barH)
  }
  ctx.fillStyle = THEME.text
  ctx.font = THEME.mono
  ctx.textAlign = 'right'
  ctx.textBaseline = 'middle'
  ctx.fillText(formatSoc(metrics.soc), w - pad, barY + barH / 2 + 2)

  // 底部：温度与功率
  ctx.font = THEME.sansSmall
  ctx.textAlign = 'left'
  ctx.fillStyle = THEME.dim
  ctx.fillText('最高温度', pad, h - pad - 52)
  ctx.fillText('功率', pad, h - pad)
  ctx.fillStyle = THEME.text
  ctx.font = THEME.mono
  ctx.textBaseline = 'top'
  ctx.fillText(formatTemp(metrics.maxTemp), pad + 150, h - pad - 56)
  ctx.fillText(formatPower(metrics.power), pad + 150, h - pad - 4)
}

// createPanel 为一台设备创建可重复更新的面板纹理。
export function createPanel(device, w = 512, h = 256) {
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d')
  const update = (metrics) => drawPanel(ctx, device, metrics, w, h)
  update(null)
  return { canvas, update }
}
