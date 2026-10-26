import { describe, expect, it } from 'vitest'
import { drawPanel } from './panel.js'

const device = { deviceId: 'battery-cluster-01', deviceType: 'battery-cluster' }

// 记录型 2D 上下文：鸭子类型，Node 环境可跑
function recordingCtx() {
  const texts = []
  const rects = []
  return {
    texts,
    rects,
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 0,
    font: '',
    textAlign: '',
    textBaseline: '',
    fillRect(x, y, w, h) {
      this.rects.push({ x, y, w, h })
    },
    strokeRect() {},
    fillText(text) {
      texts.push(String(text))
    },
    measureText(text) {
      return { width: String(text).length * 16 }
    },
    beginPath() {},
    arc() {},
    fill() {},
  }
}

describe('drawPanel', () => {
  it('有数据时绘制设备 ID、状态与指标', () => {
    const ctx = recordingCtx()
    drawPanel(ctx, device, { soc: 0.62, maxTemp: 33.2, power: 88.4, alarmLevel: 0 }, 512, 256)
    const joined = ctx.texts.join(' | ')
    expect(joined).toContain('battery-cluster-01')
    expect(joined).toContain('正常')
    expect(joined).toContain('62%')
    expect(joined).toContain('33.2 ℃')
    expect(joined).toContain('88 kW')
  })

  it('未知告警枚举走兜底文案', () => {
    const ctx = recordingCtx()
    drawPanel(ctx, device, { alarmLevel: 99 }, 512, 256)
    expect(ctx.texts.join(' | ')).toContain('未知状态')
  })

  it('无数据时绘制等待态占位', () => {
    const ctx = recordingCtx()
    drawPanel(ctx, device, null, 512, 256)
    const joined = ctx.texts.join(' | ')
    expect(joined).toContain('battery-cluster-01')
    expect(joined).toContain('等待数据')
    expect(joined).toContain('等待实时数据…')
  })

  it('SOC 条按比例绘制（0.5 → 半宽）', () => {
    const ctx = recordingCtx()
    drawPanel(ctx, device, { soc: 0.5, alarmLevel: 0 }, 512, 256)
    const bar = ctx.rects[ctx.rects.length - 1] // 最后一个进度填充
    // 画布宽 512，边距 24，进度条总宽 464 → 50% 应为 232
    expect(bar.w).toBeCloseTo(232, 0)
  })
})
