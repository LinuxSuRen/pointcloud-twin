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
import ImportDialog from './components/ImportDialog.vue'

const viewerEl = ref(null)
const importVisible = ref(false)
/** 当前场景来自某次导入任务时的导出地址（zip / GLB） */
const exportUrls = ref(null)
let viewer = null
let pickHandler = null
let wsClient = null
let currentTileset = null
let sceneToken = 0
let panelTimer = null
/** deviceId → { entity, panel } */
const entityMap = new Map()
/** 待重绘面板的设备集合（数据帧到达时标记，定时器批量刷新） */
const panelDirty = new Set()
let config = null

const markDirty = (ids) => {
  for (const id of ids) {
    panelDirty.add(id)
  }
}

// flushPanels 批量重绘脏设备的面板纹理与描边（节流，1 秒一拍）。
const flushPanels = () => {
  for (const id of panelDirty) {
    const entry = entityMap.get(id)
    if (!entry) {
      continue
    }
    entry.panel.update(store.metrics[id])
    setEntityStatus(entry.entity, statusColor(store.metrics[id]))
  }
  panelDirty.clear()
}

const handleFrame = (frame) => {
  if (!frame) {
    return
  }
  if (frame.type === 'device-status' || frame.type === 'snapshot') {
    applyStatusFrame(frame.data)
    markDirty(Object.keys(frame.data || {}))
  }
  // heartbeat 仅用于维持连接与看门狗，无需处理
}

// loadScene 加载（或切换）一个场景：底图 + 设备代理体。
const loadScene = async (tilesUrl, devicesUrl) => {
  const token = ++sceneToken
  store.tilesState = 'loading'
  store.tilesError = ''
  store.devicesError = ''
  store.devices = []
  store.metrics = {}
  store.selectedId = null

  if (currentTileset) {
    viewer.scene.primitives.remove(currentTileset)
    currentTileset = null
  }
  viewer.entities.removeAll()
  entityMap.clear()
  panelDirty.clear()

  loadTileset(viewer, tilesUrl)
    .then((tileset) => {
      if (token !== sceneToken) {
        viewer.scene.primitives.remove(tileset)
        return
      }
      currentTileset = tileset
      store.tilesState = 'ready'
    })
    .catch(() => {
      if (token === sceneToken) {
        store.tilesState = 'error'
        store.tilesError = '点云底图加载失败，请确认转换产物已就绪且服务已启动'
      }
    })

  if (devicesUrl) {
    try {
      const devicesConfig = await getDevices(devicesUrl)
      if (token !== sceneToken) {
        return
      }
      store.devices = Array.isArray(devicesConfig?.devices) ? devicesConfig.devices : []
      const map = addDeviceEntities(viewer, store.devices, devicesConfig?.origin)
      for (const [id, entry] of map) {
        entityMap.set(id, entry)
      }
    } catch {
      if (token === sceneToken) {
        store.devicesError = '设备代理体配置加载失败，请确认 devices.json 已生成'
      }
    }
  }
}

onMounted(async () => {
  config = loadConfig()
  viewer = createViewer(viewerEl.value)

  pickHandler = setupPicking(viewer, (id) => {
    selectDevice(id)
  })

  loadScene(config.tilesUrl, config.devicesUrl)

  wsClient = createWsClient({
    url: config.wsUrl,
    onMessage: handleFrame,
    onStateChange: (state) => {
      store.wsState = state
    },
  })

  // 面板纹理按 1 秒节流批量重绘
  panelTimer = setInterval(flushPanels, 1000)
})

onUnmounted(() => {
  // 自清理：断开 WS、停掉面板重绘、销毁交互与查看器
  if (panelTimer) {
    clearInterval(panelTimer)
  }
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
  flyToDevice(viewer, entityMap.get(id)?.entity)
}

// 导入完成后切换到新转换出的场景（WebSocket 状态驱动继续生效）
const handleImportCompleted = (result) => {
  importVisible.value = false
  if (result?.tilesUrl) {
    loadScene(result.tilesUrl, result.devicesUrl || null)
  }
  exportUrls.value = result?.exportZipUrl
    ? { zip: result.exportZipUrl, glb: result.exportGlbUrl }
    : null
}

// 原生导航下载（attachment 响应不会离开页面，且避免 fetch→blob 占用内存）
const download = (url) => {
  window.location.assign(url)
}
</script>

<template>
  <div class="app-shell">
    <header class="app-header">
      <span class="app-header__brand">pointcloud-twin 查看器</span>
      <StatusBar />
      <span class="app-header__spacer"></span>
      <template v-if="exportUrls">
        <button class="app-header__btn" type="button" @click="download(exportUrls.zip)">
          导出 zip
        </button>
        <button class="app-header__btn" type="button" @click="download(exportUrls.glb)">
          导出 GLB
        </button>
      </template>
      <button class="app-header__btn" type="button" @click="importVisible = true">导入 PCD</button>
    </header>
    <aside class="sidebar">
      <DeviceList @select="handleSelect" />
    </aside>
    <main class="app-main">
      <div ref="viewerEl" class="viewer-container"></div>
      <DevicePanel v-if="store.selectedId" :device-id="store.selectedId" class="device-panel" />
      <ImportDialog
        v-if="importVisible"
        :api-base="config?.apiBase || '/api'"
        @close="importVisible = false"
        @completed="handleImportCompleted"
      />
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

.app-header__spacer {
  flex: 1;
}

.app-header__btn {
  background: var(--color-bg-elevated);
  border: 1px solid var(--color-accent);
  color: var(--color-accent);
  border-radius: 6px;
  padding: 4px 12px;
  cursor: pointer;
  font-size: 13px;
}

.app-header__btn:hover {
  background: var(--color-accent);
  color: #fff;
}
</style>
