// 模拟数据源：静态托管转换产物（3D Tiles + devices.json），并通过 WebSocket
// 按消息契约推送设备实时状态，用于本地演示查看器。
//
// 用法：node mock/server.mjs --root ../out --port 8787 --interval 1000
import http from 'node:http'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { WebSocketServer } from 'ws'

const args = process.argv.slice(2)
const argOf = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i >= 0 && args[i + 1] ? args[i + 1] : fallback
}
const root = path.resolve(process.cwd(), argOf('root', '../out'))
const port = Number(argOf('port', '8787'))
const interval = Number(argOf('interval', '1000'))

const CONTENT_TYPES = {
  '.json': 'application/json',
  '.glb': 'model/gltf-binary',
  '.pnts': 'application/octet-stream',
  '.las': 'application/octet-stream',
}

// 读取设备清单
let deviceIds = []
try {
  const cfg = JSON.parse(await readFile(path.join(root, 'devices.json'), 'utf8'))
  deviceIds = (cfg.devices || []).map((d) => d.deviceId)
} catch {
  console.warn('未找到 devices.json，WebSocket 只推送心跳')
}
if (deviceIds.length > 0) {
  console.log(`已加载 ${deviceIds.length} 台设备：${deviceIds.join(', ')}`)
}

// 模拟状态随机游走
const state = new Map()
const clamp = (v, min, max) => Math.min(max, Math.max(min, v))
const walk = (v, step, min, max) => clamp(v + (Math.random() - 0.5) * 2 * step, min, max)
for (const id of deviceIds) {
  state.set(id, { soc: 0.5 + Math.random() * 0.4, maxTemp: 28 + Math.random() * 4, power: Math.random() * 120 })
}

function tick() {
  for (const id of deviceIds) {
    const s = state.get(id)
    s.soc = walk(s.soc, 0.008, 0.05, 1)
    s.maxTemp = walk(s.maxTemp, 0.4, 22, 46)
    s.power = walk(s.power, 8, -60, 320)
    let alarmLevel = 0
    if (s.maxTemp >= 42) {
      alarmLevel = 3
    } else if (s.maxTemp >= 38) {
      alarmLevel = 2
    } else if (s.maxTemp >= 34 || s.soc <= 0.15) {
      alarmLevel = 1
    }
    s.alarmLevel = alarmLevel
    s.ts = Date.now()
  }
}

function frame(type, data) {
  return JSON.stringify({ type, ts: Date.now(), data })
}

const server = http.createServer(async (req, res) => {
  if (!req.url.startsWith('/tiles/')) {
    res.writeHead(404).end('not found')
    return
  }
  const rel = decodeURIComponent(req.url.slice('/tiles/'.length)).replaceAll('\\', '/')
  const file = path.normalize(path.join(root, rel))
  if (!file.startsWith(root)) {
    res.writeHead(403).end('forbidden')
    return
  }
  try {
    const data = await readFile(file)
    res.writeHead(200, {
      'content-type': CONTENT_TYPES[path.extname(file)] || 'application/octet-stream',
      'access-control-allow-origin': '*',
    })
    res.end(data)
  } catch {
    res.writeHead(404).end('not found')
  }
})

const wss = new WebSocketServer({ server })

wss.on('connection', (ws) => {
  console.log('WebSocket 客户端已连接')
  tick()
  if (deviceIds.length > 0) {
    ws.send(frame('snapshot', Object.fromEntries(state)))
  }
  const statusTimer = setInterval(() => {
    if (ws.readyState !== 1 || deviceIds.length === 0) {
      return
    }
    tick()
    ws.send(frame('device-status', Object.fromEntries(state)))
  }, interval)
  const heartbeatTimer = setInterval(() => {
    if (ws.readyState === 1) {
      ws.send(frame('heartbeat', null))
    }
  }, 15000)
  ws.on('close', () => {
    clearInterval(statusTimer)
    clearInterval(heartbeatTimer)
    console.log('WebSocket 客户端已断开')
  })
})

server.listen(port, () => {
  console.log(`模拟数据源已启动：`)
  console.log(`  瓦片与配置 http://127.0.0.1:${port}/tiles/tileset.json`)
  console.log(`  WebSocket    ws://127.0.0.1:${port}/ws`)
  console.log(`  数据目录     ${root}`)
})
