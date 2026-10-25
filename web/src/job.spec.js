import { afterEach, describe, expect, it, vi } from 'vitest'
import { cancelJob, createJob, getJob, subscribeJobEvents, isTerminal } from './api.js'

afterEach(() => {
  vi.unstubAllGlobals()
})

const okJob = { id: 'j1', state: 'running', progress: { stage: 'load', percent: 10, message: 'x' } }

describe('任务 API', () => {
  it('createJob 成功返回任务视图', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: true, status: 202, json: () => Promise.resolve(okJob) })),
    )
    const job = await createJob('/api', { pcdUploadId: 'u1' })
    expect(job.id).toBe('j1')
  })

  it('createJob 409 携带服务端文案', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: false,
          status: 409,
          json: () => Promise.resolve({ error: { code: 'E7001', message: '已有转换任务在运行，请稍后再试' } }),
        }),
      ),
    )
    await expect(createJob('/api', {})).rejects.toThrow('已有转换任务在运行，请稍后再试')
  })

  it('getJob 网络错误给友好文案', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('down'))))
    await expect(getJob('/api', 'j1')).rejects.toThrow('无法连接转换服务')
  })

  it('cancelJob 透传 DELETE', async () => {
    const spy = vi.fn(() => Promise.resolve({ ok: true, status: 202, json: () => Promise.resolve({ cancelling: true }) }))
    vi.stubGlobal('fetch', spy)
    await cancelJob('/api', 'j1')
    expect(spy.mock.calls[0][0]).toContain('/api/jobs/j1')
    expect(spy.mock.calls[0][1].method).toBe('DELETE')
  })
})

class FakeEventSource {
  constructor(url) {
    this.url = url
    this.listeners = {}
    this.closed = false
  }
  addEventListener(type, cb) {
    ;(this.listeners[type] = this.listeners[type] || []).push(cb)
  }
  emit(type, payload) {
    for (const cb of this.listeners[type] || []) {
      cb({ data: JSON.stringify(payload) })
    }
  }
  emitRaw(type, rawData) {
    for (const cb of this.listeners[type] || []) {
      cb({ data: rawData })
    }
  }
  fireError() {
    this.onerror && this.onerror()
  }
  close() {
    this.closed = true
  }
}

describe('subscribeJobEvents（一次性任务语义）', () => {
  it('终态即 close + onEnd', () => {
    const es = new FakeEventSource('/api/jobs/j1/events')
    const done = vi.fn()
    const sub = subscribeJobEvents('/api', 'j1', { onEvent: vi.fn(), onEnd: done }, () => es)
    es.emit('job', { id: 'j1', state: 'running' })
    expect(es.closed).toBe(false)
    es.emit('job', { id: 'j1', state: 'succeeded', result: { tilesUrl: '/t' } })
    expect(es.closed).toBe(true)
    expect(done).toHaveBeenCalledWith(expect.objectContaining({ state: 'succeeded' }))
    sub.close()
  })

  it('坏帧静默忽略，不杀流', () => {
    const es = new FakeEventSource('/x')
    const sub = subscribeJobEvents('/api', 'j1', { onEvent: vi.fn(), onEnd: vi.fn() }, () => es)
    expect(() => es.emitRaw('job', '{bad json')).not.toThrow()
    expect(es.closed).toBe(false)
    sub.close()
  })

  it('出错即 close + onError（调用方转 REST 兜底）', () => {
    const es = new FakeEventSource('/x')
    const onError = vi.fn()
    const sub = subscribeJobEvents('/api', 'j1', { onError }, () => es)
    es.fireError()
    expect(es.closed).toBe(true)
    expect(onError).toHaveBeenCalled()
    sub.close()
  })

  it('isTerminal 识别三种终态', () => {
    expect(isTerminal('succeeded')).toBe(true)
    expect(isTerminal('failed')).toBe(true)
    expect(isTerminal('cancelled')).toBe(true)
    expect(isTerminal('running')).toBe(false)
    expect(isTerminal(undefined)).toBe(false)
  })
})
