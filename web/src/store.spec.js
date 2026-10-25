import { describe, expect, it } from 'vitest'
import { applyStatusFrame, selectDevice, store } from './store.js'

describe('store', () => {
  it('增量合并状态帧', () => {
    applyStatusFrame({ 'pcs-01': { soc: 0.5, maxTemp: 30 } })
    applyStatusFrame({ 'pcs-01': { soc: 0.6 } })
    expect(store.metrics['pcs-01']).toEqual({ soc: 0.6, maxTemp: 30 })
  })

  it('容错非法数据', () => {
    expect(() => applyStatusFrame(null)).not.toThrow()
    expect(() => applyStatusFrame({ bad: 1 })).not.toThrow()
  })

  it('选中设备', () => {
    selectDevice('pcs-01')
    expect(store.selectedId).toBe('pcs-01')
    selectDevice(null)
    expect(store.selectedId).toBeNull()
  })
})
