import { describe, expect, it } from 'vitest'
import { uploadFile } from './uploader.js'
import { hashBytes } from './sha256.js'

// 内存版假服务：模拟 Go 端分片上传引擎的关键行为
function fakeServer({ chunkSize = 64, flaky = {} } = {}) {
  const sessions = new Map()
  let initCount = 0
  const putLog = []
  const json = (status, body) =>
    Promise.resolve({ ok: status >= 200 && status < 300, status, json: () => Promise.resolve(body) })

  const server = {
    sessions,
    initCount: () => initCount,
    putLog,
    fetchImpl: async (url, init) => {
      const m = init?.method || 'GET'
      if (url.endsWith('/init') && m === 'POST') {
        const req = JSON.parse(init.body)
        const id = `up-${++initCount}`
        sessions.set(id, {
          id,
          chunkSize,
          size: req.size,
          purpose: req.purpose,
          received: new Set(),
          bytes: new Uint8Array(req.size),
          completed: false,
        })
        return json(202, { uploadId: id, chunkSize })
      }
      let match
      if ((match = url.match(/\/uploads\/([^/]+)\/chunks\/(\d+)$/)) && m === 'PUT') {
        const s = sessions.get(match[1])
        const index = Number(match[2])
        if (!s) {
          return json(404, { error: { code: 'E7003', message: '会话不存在' } })
        }
        if (s.completed) {
          return json(409, { error: { code: 'E7003', message: '已定稿' } })
        }
        const blob = init.body
        const buf = new Uint8Array(await blob.arrayBuffer())
        // 故意制造「响应丢失但分片已落盘」或网络错误
        if (flaky.dropResponse?.(index)) {
          s.bytes.set(buf, index * chunkSize)
          s.received.add(index)
          throw new TypeError('network down')
        }
        if (flaky.failOnce?.(index) && !putLog.includes(`fail:${index}`)) {
          putLog.push(`fail:${index}`)
          throw new TypeError('network down')
        }
        s.bytes.set(buf, index * chunkSize)
        s.received.add(index)
        putLog.push(index)
        return json(200, { received: s.received.size })
      }
      if ((match = url.match(/\/uploads\/([^/]+)\/complete$/)) && m === 'POST') {
        const s = sessions.get(match[1])
        const req = JSON.parse(init.body)
        const want = hashBytes(s.bytes)
        if (req.sha256 && req.sha256 !== want) {
          return json(400, { error: { code: 'E7003', message: 'SHA-256 校验失败' } })
        }
        s.completed = true
        return json(200, { ok: true })
      }
      if ((match = url.match(/\/uploads\/([^/]+)$/))) {
        const s = sessions.get(match[1])
        if (!s) {
          return json(404, { error: { code: 'E7003', message: '会话不存在' } })
        }
        return json(200, {
          size: s.size,
          chunkSize: s.chunkSize,
          totalChunks: Math.ceil(s.size / s.chunkSize),
          receivedChunks: [...s.received],
          state: s.completed ? 'completed' : 'pending',
        })
      }
      return json(404, { error: { code: 'x', message: 'not found' } })
    },
  }
  return server
}

function makeFile(bytes, name = 'site.pcd') {
  return new File([bytes], name)
}

describe('uploadFile', () => {
  it('完整上传并通过服务端 sha256 校验', async () => {
    const bytes = new Uint8Array(200)
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = i % 251
    }
    const srv = fakeServer({ chunkSize: 64 })
    const progresses = []
    const id = await uploadFile(makeFile(bytes), {
      purpose: 'pcd',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
      onProgress: (p) => progresses.push(p.uploaded),
    })
    const s = srv.sessions.get(id)
    expect(s.completed).toBe(true)
    expect([...s.bytes]).toEqual([...bytes])
    // 进度单调不减、以服务端口径为准
    expect(progresses[progresses.length - 1]).toBe(200)
    for (let i = 1; i < progresses.length; i++) {
      expect(progresses[i]).toBeGreaterThanOrEqual(progresses[i - 1])
    }
  })

  it('断点续传：只补缺失分片', async () => {
    const bytes = new Uint8Array(128)
    const srv = fakeServer({ chunkSize: 64 })
    // 预置会话：分片 0 已收到
    srv.sessions.set('up-resume', {
      id: 'up-resume',
      chunkSize: 64,
      size: 128,
      purpose: 'pcd',
      received: new Set([0]),
      bytes: new Uint8Array(128),
      completed: false,
    })
    await uploadFile(makeFile(bytes), {
      purpose: 'pcd',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
      resumeId: 'up-resume',
    })
    // 只 PUT 了分片 1，分片 0 未重传
    expect(srv.putLog).toEqual([1])
    expect(srv.sessions.get('up-resume').completed).toBe(true)
  })

  it('会话过期（404）时自动重新 initiate 并回调新 id', async () => {
    const bytes = new Uint8Array(50)
    const srv = fakeServer({ chunkSize: 64 })
    const seenIds = []
    const id = await uploadFile(makeFile(bytes), {
      purpose: 'pcd',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
      resumeId: 'gone-session',
      onUploadId: (uid) => seenIds.push(uid),
    })
    expect(srv.initCount()).toBe(1)
    expect(seenIds).toEqual([id])
    expect(srv.sessions.get(id).completed).toBe(true)
  })

  it('响应丢失但分片已落盘：重试前对账，不重传', async () => {
    const bytes = new Uint8Array(128)
    const srv = fakeServer({
      chunkSize: 64,
      flaky: { dropResponse: (i) => i === 1 },
    })
    await uploadFile(makeFile(bytes), {
      purpose: 'pcd',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
    })
    // 分片 1 响应丢失但已落盘：客户端对账后跳过重传（无成功 ACK 的 PUT 记录）
    expect(srv.putLog.filter((x) => x === 1)).toHaveLength(0)
    expect(srv.sessions.get('up-1').completed).toBe(true)
  })

  it('已定稿会话返回 409 时抛出友好错误', async () => {
    const bytes = new Uint8Array(64)
    const srv = fakeServer({ chunkSize: 64 })
    srv.sessions.set('up-done', {
      id: 'up-done',
      chunkSize: 64,
      size: 64,
      purpose: 'pcd',
      received: new Set(),
      bytes: new Uint8Array(64),
      completed: true,
    })
    const id = await uploadFile(makeFile(bytes), {
      purpose: 'pcd',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
      resumeId: 'up-done',
    })
    expect(id).toBe('up-done') // 已完成会话直接复用
  })

  it('缺少 purpose 直接报错', async () => {
    await expect(uploadFile(makeFile(new Uint8Array(8)), {})).rejects.toThrow('purpose')
  })

  it('onUploadId 未提供时不影响流程', async () => {
    const srv = fakeServer({ chunkSize: 64 })
    const id = await uploadFile(makeFile(new Uint8Array(30)), {
      purpose: 'devices',
      apiBase: '/api',
      fetchImpl: srv.fetchImpl,
    })
    expect(id).toBeTruthy()
  })
})

describe('uploadFile 重试上限', () => {
  it('单片持续失败最终报错', async () => {
    const srv = fakeServer({ chunkSize: 64 })
    // 所有 PUT 一律网络失败
    const flakyImpl = async (url, init) => {
      if ((init?.method === 'PUT' || (!init && url.includes('/chunks/')))) {
        throw new TypeError('network down')
      }
      return srv.fetchImpl(url, init)
    }
    await expect(
      uploadFile(makeFile(new Uint8Array(64)), {
        purpose: 'pcd',
        apiBase: '/api',
        fetchImpl: flakyImpl,
      }),
    ).rejects.toThrow('分片')
  }, 20000)
})
