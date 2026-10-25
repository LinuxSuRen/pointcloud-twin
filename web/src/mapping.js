// 设备状态的展示映射：后端/数据源只给稳定枚举与数值，
// 用户可见文案全部由前端映射表渲染，未知枚举走兜底文案。

export const ALARM_LEVEL_TEXT = {
  0: '正常',
  1: '注意',
  2: '警告',
  3: '严重',
}

export const ALARM_UNKNOWN_TEXT = '未知状态'

export const DEVICE_TYPE_TEXT = {
  'battery-cluster': '电池舱',
  pcs: 'PCS 柜',
  transformer: '变压器',
}

export function alarmText(level) {
  return ALARM_LEVEL_TEXT[level] ?? ALARM_UNKNOWN_TEXT
}

export function deviceTypeText(type) {
  return DEVICE_TYPE_TEXT[type] ?? type
}

// 状态展示色（具体色值，同时供 DOM 与 Cesium 材质使用）。
export const STATUS_COLORS = {
  normal: '#27c93f',
  warning: '#ffbd2e',
  danger: '#ff5f56',
  unknown: '#5d6b7d',
}

// statusColor 依据告警等级与关键阈值给出展示颜色。
// 规则：严重/警告 > 注意 > SOC 低电量 > 正常。
export function statusColor(metrics) {
  if (!metrics) {
    return STATUS_COLORS.unknown
  }
  if (metrics.alarmLevel >= 2) {
    return STATUS_COLORS.danger
  }
  if (metrics.alarmLevel === 1 || (typeof metrics.soc === 'number' && metrics.soc < 0.2)) {
    return STATUS_COLORS.warning
  }
  return STATUS_COLORS.normal
}

// formatSoc 把 0~1 的 SOC 值格式化为百分比；缺省显示 —。
export function formatSoc(soc) {
  if (typeof soc !== 'number' || Number.isNaN(soc)) {
    return '—'
  }
  return `${Math.round(soc * 100)}%`
}

export function formatTemp(temp) {
  if (typeof temp !== 'number' || Number.isNaN(temp)) {
    return '—'
  }
  return `${temp.toFixed(1)} ℃`
}

export function formatPower(power) {
  if (typeof power !== 'number' || Number.isNaN(power)) {
    return '—'
  }
  return `${power.toFixed(0)} kW`
}

export function formatTime(ts) {
  if (!ts) {
    return '—'
  }
  return new Date(ts).toLocaleTimeString('zh-CN', { hour12: false })
}
