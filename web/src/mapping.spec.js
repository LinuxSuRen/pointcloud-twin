import { describe, expect, it } from 'vitest'
import {
  alarmText,
  deviceTypeText,
  formatPower,
  formatSoc,
  formatTemp,
  statusColor,
  STATUS_COLORS,
} from './mapping.js'

describe('alarmText', () => {
  it('渲染已知枚举', () => {
    expect(alarmText(0)).toBe('正常')
    expect(alarmText(1)).toBe('注意')
    expect(alarmText(2)).toBe('警告')
    expect(alarmText(3)).toBe('严重')
  })

  it('未知枚举走兜底文案', () => {
    expect(alarmText(99)).toBe('未知状态')
    expect(alarmText(undefined)).toBe('未知状态')
  })
})

describe('deviceTypeText', () => {
  it('渲染已知类型', () => {
    expect(deviceTypeText('battery-cluster')).toBe('电池舱')
    expect(deviceTypeText('pcs')).toBe('PCS 柜')
  })

  it('未知类型回退展示原始标识', () => {
    expect(deviceTypeText('fire-hydrant')).toBe('fire-hydrant')
  })
})

describe('statusColor', () => {
  it('无数据用未知色', () => {
    expect(statusColor(null)).toBe(STATUS_COLORS.unknown)
  })

  it('告警等级优先', () => {
    expect(statusColor({ alarmLevel: 2 })).toBe(STATUS_COLORS.danger)
    expect(statusColor({ alarmLevel: 3 })).toBe(STATUS_COLORS.danger)
    expect(statusColor({ alarmLevel: 1 })).toBe(STATUS_COLORS.warning)
  })

  it('SOC 低电量用注意色', () => {
    expect(statusColor({ alarmLevel: 0, soc: 0.1 })).toBe(STATUS_COLORS.warning)
    expect(statusColor({ alarmLevel: 0, soc: 0.8 })).toBe(STATUS_COLORS.normal)
  })
})

describe('指标格式化', () => {
  it('正常值', () => {
    expect(formatSoc(0.826)).toBe('83%')
    expect(formatTemp(31.44)).toBe('31.4 ℃')
    expect(formatPower(120.4)).toBe('120 kW')
  })

  it('缺省值显示占位符', () => {
    expect(formatSoc(undefined)).toBe('—')
    expect(formatTemp(undefined)).toBe('—')
    expect(formatPower(NaN)).toBe('—')
  })
})
