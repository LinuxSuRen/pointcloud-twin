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
  }
}

// getDevices 拉取设备代理体配置。
export function getDevices(url) {
  return request(url)
}
