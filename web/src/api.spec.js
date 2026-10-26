import { afterEach, describe, expect, it, vi } from 'vitest'
import { getDevices, loadConfig } from './api.js'

afterEach(() => {
  vi.unstubAllGlobals()
})

const fakeLocation = { protocol: 'http:', host: '127.0.0.1:5173' }

describe('loadConfig', () => {
  it('缺省值', () => {
    const cfg = loadConfig('?', fakeLocation)
    expect(cfg.tilesUrl).toBe('/tiles/tileset.json')
    expect(cfg.devicesUrl).toBe('/tiles/devices.json')
    expect(cfg.wsUrl).toBe('ws://127.0.0.1:5173/ws')
  })

  it('覆盖瓦片与 WebSocket 地址', () => {
    const cfg = loadConfig('?tiles=/t/tileset.json&devices=/t/devices.json&ws=ws://1.2.3.4:9/ws', fakeLocation)
    expect(cfg.tilesUrl).toBe('/t/tileset.json')
    expect(cfg.devicesUrl).toBe('/t/devices.json')
    expect(cfg.wsUrl).toBe('ws://1.2.3.4:9/ws')
  })
})

describe('getDevices', () => {
  it('正常返回解析后的 JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ devices: [] }) })),
    )
    const cfg = await getDevices('/tiles/devices.json')
    expect(cfg).toEqual({ devices: [] })
  })

  it('HTTP 错误给友好文案', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false, status: 404 })))
    await expect(getDevices('/tiles/devices.json')).rejects.toThrow('请求失败（HTTP 404）')
  })

  it('网络错误给友好文案', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('failed'))))
    await expect(getDevices('/tiles/devices.json')).rejects.toThrow('无法连接数据服务，请确认服务已启动')
  })
})
