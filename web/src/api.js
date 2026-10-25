// 唯一的 HTTP 封装：超时、网络错误友好包装、非 2xx 兜底文案。
// 数据源是转换产物（静态 JSON），不涉及鉴权。

export class ApiError extends Error {
  constructor(message, code) {
    super(message)
    this.code = code
  }
}

const TIMEOUT_MS = 15000

async function request(path) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)
  let resp
  try {
    resp = await fetch(path, { signal: controller.signal })
  } catch (err) {
    if (err?.name === 'AbortError') {
      throw new ApiError('请求超时，请检查网络后重试', 'timeout')
    }
    throw new ApiError('无法连接数据服务，请确认服务已启动', 'network')
  } finally {
    clearTimeout(timer)
  }
  if (!resp.ok) {
    throw new ApiError(`请求失败（HTTP ${resp.status}）`, `http_${resp.status}`)
  }
  try {
    return await resp.json()
  } catch {
    throw new ApiError('数据格式异常，请确认转换产物完整', 'bad_json')
  }
}

// loadConfig 解析查看器运行参数。search/location 可注入，便于在 Node 环境测试。
export function loadConfig(search, location) {
  const s = search ?? (typeof window !== 'undefined' ? window.location.search : '')
  const loc = location ?? (typeof window !== 'undefined' ? window.location : { protocol: 'http:', host: 'localhost' })
  const params = new URLSearchParams(s)
  const wsProtocol = loc.protocol === 'https:' ? 'wss:' : 'ws:'
  return {
    tilesUrl: params.get('tiles') || '/tiles/tileset.json',
    devicesUrl: params.get('devices') || '/tiles/devices.json',
    wsUrl: params.get('ws') || `${wsProtocol}//${loc.host}/ws`,
    apiBase: params.get('api') || '/api',
  }
}

// getDevices 拉取设备代理体配置。
export function getDevices(url) {
  return request(url)
}

// ---- 转换任务 API（pc2twin serve）----

export const JOB_TERMINAL_STATES = ['succeeded', 'failed', 'cancelled']
export const isTerminal = (state) => JOB_TERMINAL_STATES.includes(state)

async function jobRequest(url, method, body) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15000)
  let resp
  try {
    resp = await fetch(url, {
      method,
      signal: controller.signal,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch (err) {
    if (err?.name === 'AbortError') {
      throw new ApiError('请求超时，请检查网络后重试', 'timeout')
    }
    throw new ApiError('无法连接转换服务，请确认 pc2twin serve 已启动', 'network')
  } finally {
    clearTimeout(timer)
  }
  const data = await resp.json().catch(() => null)
  if (!resp.ok) {
    throw new ApiError(data?.error?.message || `请求失败（HTTP ${resp.status}）`, data?.error?.code || `http_${resp.status}`)
  }
  return data
}

// createJob 提交转换任务（202 + jobId）。
export function createJob(apiBase, payload) {
  return jobRequest(`${apiBase}/jobs`, 'POST', payload)
}

// getJob 查询任务状态（SSE 断开时的兜底）。
export function getJob(apiBase, id) {
  return jobRequest(`${apiBase}/jobs/${encodeURIComponent(id)}`, 'GET')
}

// cancelJob 请求取消任务。
export function cancelJob(apiBase, id) {
  return jobRequest(`${apiBase}/jobs/${encodeURIComponent(id)}`, 'DELETE')
}

// saveDevices 把查看器内编辑后的设备代理体配置写回任务产物。
export function saveDevices(apiBase, jobId, config) {
  return jobRequest(
    `${apiBase}/jobs/${encodeURIComponent(jobId)}/out/devices.json`,
    'PUT',
    config,
  )
}

/**
 * subscribeJobEvents 订阅任务 SSE 进度流（一次性任务语义）：
 * 收到终态即 close + onEnd；出错即 close + onError（调用方转 REST 轮询兜底）。
 */
export function subscribeJobEvents(apiBase, id, handlers, factory) {
  const createSource = factory || ((u) => new EventSource(u))
  const es = createSource(`${apiBase}/jobs/${encodeURIComponent(id)}/events`)
  let ended = false
  const close = () => {
    if (!ended) {
      ended = true
      es.close()
    }
  }
  es.addEventListener('job', (event) => {
    let view
    try {
      view = JSON.parse(event.data)
    } catch {
      return // 单条坏消息不能杀整条流
    }
    if (handlers.onEvent) {
      handlers.onEvent(view)
    }
    if (isTerminal(view.state)) {
      close()
      if (handlers.onEnd) {
        handlers.onEnd(view)
      }
    }
  })
  es.onerror = () => {
    close()
    if (handlers.onError) {
      handlers.onError()
    }
  }
  return { close }
}
