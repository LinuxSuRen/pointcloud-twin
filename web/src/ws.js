// WebSocket 客户端：自动重连（指数退避）、心跳看门狗、手动关闭。
// 消息契约见 docs/design.md：{type: 'device-status'|'snapshot'|'heartbeat', ts, data}

const RECONNECT_BASE_MS = 2000
const RECONNECT_MAX_MS = 30000
const HEARTBEAT_TIMEOUT_MS = 30000

export function createWsClient({ url, onMessage, onStateChange, socketFactory }) {
  const factory = socketFactory || ((u) => new WebSocket(u))
  let socket = null
  let closedByUser = false
  let attempts = 0
  let reconnectTimer = null
  let heartbeatTimer = null

  const setState = (state) => {
    if (onStateChange) {
      onStateChange(state)
    }
  }

  const scheduleReconnect = () => {
    if (closedByUser) {
      return
    }
    attempts += 1
    const delay = Math.min(RECONNECT_BASE_MS * 2 ** (attempts - 1), RECONNECT_MAX_MS)
    setState('reconnecting')
    reconnectTimer = setTimeout(connect, delay)
  }

  const armHeartbeatWatchdog = () => {
    clearTimeout(heartbeatTimer)
    heartbeatTimer = setTimeout(() => {
      // 心跳超时视为连接失效，主动断开触发重连
      if (socket && socket.readyState === 1) {
        socket.close()
      }
    }, HEARTBEAT_TIMEOUT_MS)
  }

  const connect = () => {
    setState(attempts === 0 ? 'connecting' : 'reconnecting')
    socket = factory(url)

    socket.onopen = () => {
      attempts = 0
      setState('open')
      armHeartbeatWatchdog()
    }
    socket.onmessage = (event) => {
      armHeartbeatWatchdog()
      let frame
      try {
        frame = JSON.parse(event.data)
      } catch {
        return
      }
      if (onMessage) {
        onMessage(frame)
      }
    }
    socket.onclose = () => {
      clearTimeout(heartbeatTimer)
      if (closedByUser) {
        setState('closed')
        return
      }
      scheduleReconnect()
    }
    socket.onerror = () => {
      // 出错后必然触发 onclose，由其统一处理重连
    }
  }

  connect()

  return {
    close() {
      closedByUser = true
      clearTimeout(reconnectTimer)
      clearTimeout(heartbeatTimer)
      if (socket) {
        socket.close()
      }
      setState('closed')
    },
  }
}
