import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createWsClient } from './ws.js'

// 可控的假 WebSocket：readyState 1 = OPEN
class FakeSocket {
  static created = []
  constructor(url) {
    this.url = url
    this.readyState = 0
    this.onopen = null
    this.onmessage = null
    this.onclose = null
    this.onerror = null
    FakeSocket.created.push(this)
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  message(data) {
    this.onmessage?.({ data })
  }
  close() {
    if (this.readyState === 3) {
      return
    }
    this.readyState = 3
    this.onclose?.()
  }
}

const factory = (url) => new FakeSocket(url)

beforeEach(() => {
  FakeSocket.created = []
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('createWsClient', () => {
  it('连接成功后进入 open 并接收状态帧', () => {
    const frames = []
    const client = createWsClient({
      url: 'ws://test/ws',
      onMessage: (f) => frames.push(f),
      socketFactory: factory,
    })
    const socket = FakeSocket.created[0]
    socket.open()
    const payload = JSON.stringify({ type: 'device-status', ts: 1, data: { 'pcs-01': { soc: 0.5 } } })
    socket.message(payload)
    const want = [{ type: 'device-status', ts: 1, data: { 'pcs-01': { soc: 0.5 } } }]
    expect(frames).toEqual(want)
    client.close()
  })

  it('快照帧与非法消息容错', () => {
    const frames = []
    const client = createWsClient({
      url: 'ws://test/ws',
      onMessage: (f) => frames.push(f),
      socketFactory: factory,
    })
    const socket = FakeSocket.created[0]
    socket.open()
    socket.message('{bad json')
    socket.message(JSON.stringify({ type: 'snapshot', ts: 2, data: {} }))
    expect(frames).toHaveLength(1)
    client.close()
  })

  it('意外断开后按退避重连', () => {
    const states = []
    const client = createWsClient({
      url: 'ws://test/ws',
      onStateChange: (s) => states.push(s),
      socketFactory: factory,
    })
    expect(FakeSocket.created).toHaveLength(1)
    FakeSocket.created[0].open()
    FakeSocket.created[0].close() // 意外断开
    vi.advanceTimersByTime(2000)
    expect(FakeSocket.created).toHaveLength(2)
    expect(states).toContain('reconnecting')
    client.close()
  })

  it('手动 close 后不再重连', () => {
    const client = createWsClient({ url: 'ws://test/ws', socketFactory: factory })
    const socket = FakeSocket.created[0]
    socket.open()
    client.close()
    vi.advanceTimersByTime(60000)
    expect(FakeSocket.created).toHaveLength(1)
  })

  it('心跳看门狗超时触发重连', () => {
    const client = createWsClient({ url: 'ws://test/ws', socketFactory: factory })
    const socket = FakeSocket.created[0]
    socket.open()
    // 30 秒无任何消息 → 看门狗主动断开 → 重连
    vi.advanceTimersByTime(30000 + 2000)
    expect(FakeSocket.created).toHaveLength(2)
    client.close()
  })

  it('收到消息会重置心跳看门狗', () => {
    const client = createWsClient({ url: 'ws://test/ws', socketFactory: factory })
    const socket = FakeSocket.created[0]
    socket.open()
    vi.advanceTimersByTime(29000)
    socket.message(JSON.stringify({ type: 'heartbeat', ts: 3, data: null }))
    vi.advanceTimersByTime(29000)
    expect(FakeSocket.created).toHaveLength(1)
    client.close()
  })
})
