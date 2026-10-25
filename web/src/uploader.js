// 分片上传客户端引擎：
//   - 并发窗口（默认 3，夹紧 1~6）+ 单片重试（3 次），重试前先查 status 对账，
//     已确认收到的分片直接跳过（响应丢失 ≠ 分片丢失）
//   - 会话过期（404）自动重新 initiate，经 onUploadId 回调持久化（刷新页面可续传）
//   - 大文件 sha256 流式计算（与分片上传并行）
//   - 进度以服务端 received_chunks 口径回调（重试/断点后仍准确）
import { hashFile } from './sha256.js'

const RETRY_TIMES = 3
const RETRY_BASE_MS = 300

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const clamp = (v, min, max) => Math.min(max, Math.max(min, v))

class UploaderError extends Error {
  constructor(message, code) {
    super(message)
    this.code = code
  }
}

/**
 * uploadFile 把文件经分片通道上传并定稿。
 * @returns {Promise<string>} uploadId
 */
export async function uploadFile(file, opts) {
  const {
    purpose,
    apiBase = '/api',
    concurrency = 3,
    onProgress,
    onUploadId,
    resumeId = null,
    fetchImpl,
  } = opts || {}
  if (!purpose) {
    throw new UploaderError('缺少上传 purpose', 'bad_request')
  }
  const doFetch =
    fetchImpl ||
    ((url, init) => fetch(url, init))

  const jsonOf = async (resp) => {
    const body = await resp.json().catch(() => null)
    if (!resp.ok) {
      const message = body?.error?.message || `上传请求失败（HTTP ${resp.status}）`
      throw new UploaderError(message, body?.error?.code || `http_${resp.status}`)
    }
    return body
  }

  const initiate = async () => {
    const resp = await doFetch(`${apiBase}/uploads/init`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filename: file.name, size: file.size, purpose }),
    })
    const body = await jsonOf(resp)
    if (onUploadId) {
      onUploadId(body.uploadId)
    }
    return body.uploadId
  }

  const fetchStatus = async (id) => {
    const resp = await doFetch(`${apiBase}/uploads/${id}`, { method: 'GET' })
    if (resp.status === 404) {
      return null
    }
    return jsonOf(resp)
  }

  // 断点续传：优先复用外部持久化的 uploadId，404 则重新 initiate
  let uploadId = resumeId
  let status = null
  if (uploadId) {
    status = await fetchStatus(uploadId).catch(() => null)
  }
  if (!status) {
    uploadId = await initiate()
    status = await fetchStatus(uploadId)
  }
  if (status.state === 'completed') {
    if (onProgress) {
      onProgress({ uploaded: file.size, total: file.size })
    }
    return uploadId
  }

  const chunkSize = status.chunkSize
  const totalChunks = Math.ceil(file.size / chunkSize)
  const received = new Set(status.receivedChunks || [])

  const chunkBytes = (index) => Math.min(chunkSize, file.size - index * chunkSize)
  const receivedBytes = () => {
    let n = 0
    for (const i of received) {
      n += chunkBytes(i)
    }
    return n
  }
  const report = () => {
    if (onProgress) {
      onProgress({ uploaded: receivedBytes(), total: file.size })
    }
  }
  report()

  const fatal = new Set([409])
  const uploadChunk = async (index) => {
    for (let attempt = 0; attempt <= RETRY_TIMES; attempt++) {
      if (received.has(index)) {
        return
      }
      try {
        const blob = file.slice(index * chunkSize, index * chunkSize + chunkBytes(index))
        const resp = await doFetch(`${apiBase}/uploads/${uploadId}/chunks/${index}`, {
          method: 'PUT',
          body: blob,
        })
        if (resp.ok) {
          received.add(index)
          report()
          return
        }
        if (fatal.has(resp.status) || resp.status === 404) {
          throw new UploaderError('上传会话已失效，请重新上传', `http_${resp.status}`)
        }
      } catch (err) {
        if (err instanceof UploaderError) {
          throw err
        }
        // 网络异常：落入重试
      }
      if (attempt === RETRY_TIMES) {
        break
      }
      await sleep(RETRY_BASE_MS * (attempt + 1))
      // 重试前先对账：确认服务端视角的分片状态
      const st = await fetchStatus(uploadId).catch(() => null)
      if (!st) {
        throw new UploaderError('上传会话已失效，请重新上传', 'http_404')
      }
      for (const i of st.receivedChunks || []) {
        received.add(i)
      }
      report()
    }
    throw new UploaderError(`分片 ${index} 上传失败，请重试`, 'chunk_failed')
  }

  // 并发窗口
  const pending = []
  for (let i = 0; i < totalChunks; i++) {
    if (!received.has(i)) {
      pending.push(i)
    }
  }
  let cursor = 0
  const width = clamp(concurrency, 1, 6)
  const workers = Array.from({ length: width }, async () => {
    while (cursor < pending.length) {
      const index = pending[cursor++]
      await uploadChunk(index)
    }
  })
  const hashPromise = hashFile(file)
  await Promise.all(workers)

  const sha = await hashPromise
  const resp = await doFetch(`${apiBase}/uploads/${uploadId}/complete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sha256: sha }),
  })
  await jsonOf(resp)
  if (onProgress) {
    onProgress({ uploaded: file.size, total: file.size })
  }
  return uploadId
}
