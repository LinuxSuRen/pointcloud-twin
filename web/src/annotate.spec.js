import { describe, expect, it } from 'vitest'
import { deviceFromCorners, uniqueDeviceId } from './annotate.js'

describe('deviceFromCorners', () => {
  it('两角点构造居中盒体', () => {
    const d = deviceFromCorners([1, 2, 0], [5, 4, 0], 3)
    expect(d.center).toEqual([3, 3, 1.5])
    expect(d.size).toEqual([4, 2, 3])
  })

  it('角点顺序无关', () => {
    const a = deviceFromCorners([1, 2, 0], [5, 4, 0])
    const b = deviceFromCorners([5, 4, 0], [1, 2, 0])
    expect(a).toEqual(b)
  })

  it('盒底取较低 Z', () => {
    const d = deviceFromCorners([0, 0, 5], [2, 2, 1], 2)
    expect(d.center[2]).toBe(2) // base=1 + 高 2 的一半
    expect(d.size[2]).toBe(2)
  })

  it('过小的框被夹到最小边长', () => {
    const d = deviceFromCorners([0, 0, 0], [0.1, 0.1, 0], 0.1)
    expect(d.size[0]).toBe(0.5)
    expect(d.size[1]).toBe(0.5)
    expect(d.size[2]).toBe(0.5)
  })
})

describe('uniqueDeviceId', () => {
  it('跳过已存在的编号', () => {
    expect(uniqueDeviceId([])).toBe('device-01')
    expect(uniqueDeviceId(['device-01'])).toBe('device-02')
    expect(uniqueDeviceId(['device-01', 'device-02'])).toBe('device-03')
  })
})
