<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { getDevices, loadConfig } from './api.js'
import { store, applyStatusFrame, selectDevice } from './store.js'
import { statusColor } from './mapping.js'
import {
  addDeviceEntities,
  createViewer,
  flyToDevice,
  loadTileset,
  setEntityStatus,
  setupPicking,
} from './cesium.js'
import { createWsClient } from './ws.js'
import StatusBar from './components/StatusBar.vue'
import DeviceList from './components/DeviceList.vue'
import DevicePanel from './components/DevicePanel.vue'

const viewerEl = ref(null)
let viewer = null
let pickHandler = null
let wsClient = null
/** deviceId → Cesium.Entity */
const entityMap = new Map()

const refreshEntityStatus = () => {
  for (const [id, entity] of entityMap) {
    setEntityStatus(entity, statusColor(store.metrics[id]))
  }
}

const handleFrame = (frame) => {
  if (!frame) {
    return
  }
  if (frame.type === 'device-status' || frame.type === 'snapshot') {
    applyStatusFrame(frame.data)
    refreshEntityStatus()
  }
  // heartbeat 仅用于维持连接与看门狗，无需处理
}

onMounted(async () => {
  const config = loadConfig()
  viewer = createViewer(viewerEl.value)

  loadTileset(viewer, config.tilesUrl)
    .then(() => {
      store.tilesState = 'ready'
    })
    .catch(() => {
      store.tilesState = 'error'
      store.tilesError = '点云底图加载失败，请确认转换产物已就绪且 mock 服务器已启动'
    })

  try {
    const devicesConfig = await getDevices(config.devicesUrl)
    store.devices = Array.isArray(devicesConfig?.devices) ? devicesConfig.devices : []
    const map = addDeviceEntities(viewer, store.devices, devicesConfig?.origin)
    for (const [id, entity] of map) {
      entityMap.set(id, entity)
    }
  } catch (err) {
    store.devicesError = '设备代理体配置加载失败，请确认 devices.json 已生成'
  }

  pickHandler = setupPicking(viewer, (id) => {
    selectDevice(id)
  })

  wsClient = createWsClient({
    url: config.wsUrl,
    onMessage: handleFrame,
    onStateChange: (state) => {
      store.wsState = state
    },
  })
})

onUnmounted(() => {
  // 自清理：断开 WS、销毁交互与查看器
  if (wsClient) {
    wsClient.close()
  }
  if (pickHandler) {
    pickHandler.destroy()
  }
  if (viewer && !viewer.isDestroyed()) {
    viewer.destroy()
  }
})

const handleSelect = (id) => {
  selectDevice(id)
  flyToDevice(viewer, entityMap.get(id))
}
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <span class="app-header__brand">pointcloud-twin 查看器</span>
      <StatusBar />
      <span class="app-header__hint">点击场景中的设备代理体查看实时状态</span>
    </header>
    <aside class="sidebar">
      <DeviceList @select="handleSelect" />
    </aside>
    <main class="app-main">
      <div ref="viewerEl" class="viewer-container"></div>
      <DevicePanel v-if="store.selectedId" :device-id="store.selectedId" class="device-panel" />
    </main>
  </div>
</template>

<style scoped>
.sidebar {
  grid-area: sidebar;
  border-right: 1px solid var(--color-border-subtle);
  background: var(--color-bg-panel);
  overflow-y: auto;
}

.device-panel {
  position: absolute;
  top: var(--space-4);
  right: var(--space-4);
  width: 280px;
  z-index: 10;
}
</style>
