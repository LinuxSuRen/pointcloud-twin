<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { getDevices, loadConfig, saveDevices } from './api.js'
import { store, applyStatusFrame, selectDevice } from './store.js'
import { statusColor } from './mapping.js'
import {
  addDeviceEntities,
  addMarkerPoint,
  addOneDevice,
  createViewer,
  enuFrame,
  flyToDevice,
  loadTileset,
  localToCartesian,
  pickLocalPoint,
  setEntityStatus,
  setupPicking,
} from './cesium.js'
import { createWsClient } from './ws.js'
import { deviceFromCorners, uniqueDeviceId } from './annotate.js'
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
let saveTimer = null
/** deviceId → { entity, panel } */
const entityMap = new Map()
/** 待重绘面板的设备集合（数据帧到达时标记，定时器批量刷新） */
const panelDirty = new Set()
let config = null
/** 当前场景的 ENU 变换与原点 */
let sceneFrame = null
let sceneOrigin = null
/** 标注模式的第一角点（局部 ENU）与临时标记 */
let annotateCorner1 = null
let annotateMarker = null

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
  store.jobId = jobIdOf(tilesUrl)
  sceneOrigin = null
  sceneFrame = enuFrame(null)
  clearAnnotateCorner()

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
      sceneOrigin = devicesConfig?.origin ?? null
      sceneFrame = enuFrame(sceneOrigin)
      store.devices = Array.isArray(devicesConfig?.devices) ? devicesConfig.devices : []
      const map = addDeviceEntities(viewer, store.devices, sceneOrigin)
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

// jobIdOf 从任务产物 URL 提取任务 ID（非任务场景返回 null）。
const jobIdOf = (tilesUrl) => {
  const m = tilesUrl?.match(/\/api\/jobs\/([^/]+)\//)
  return m ? m[1] : null
}

// ---- 设备标注：两点画盒 ----

const clearAnnotateCorner = () => {
  annotateCorner1 = null
  if (annotateMarker && !viewer?.isDestroyed?.()) {
    viewer.entities.remove(annotateMarker)
  }
  annotateMarker = null
}

const toggleAnnotate = () => {
  store.annotateMode = !store.annotateMode
  clearAnnotateCorner()
  store.annotateHint = store.annotateMode ? '在点云上点击第一个角点' : ''
}

const handleAnnotateClick = (windowPos) => {
  const local = pickLocalPoint(viewer, windowPos, sceneFrame)
  if (!local) {
    store.annotateHint = '未拾取到点云表面，请点击点云上的位置'
    return
  }
  if (!annotateCorner1) {
    annotateCorner1 = local
    annotateMarker = addMarkerPoint(viewer, localToCartesian(sceneFrame, ...local))
    store.annotateHint = '点击对角点完成画盒（Esc 取消）'
    return
  }
  // 第二角点：构造设备并生成代理体
  const geo = deviceFromCorners(annotateCorner1, local)
  const device = {
    deviceId: uniqueDeviceId(store.devices.map((d) => d.deviceId)),
    deviceType: 'battery-cluster',
    ...geo,
    heading: 0,
  }
  store.devices.push(device)
  entityMap.set(device.deviceId, addOneDevice(viewer, device, sceneFrame))
  clearAnnotateCorner()
  selectDevice(device.deviceId)
  store.annotateHint = '已创建设备，可在右侧面板编辑后保存'
}

const onKeydown = (e) => {
  if (e.key === 'Escape' && store.annotateMode) {
    toggleAnnotate()
  }
}

// ---- 设备编辑（表单实时更新） ----

const handleDeviceUpdate = ({ oldId, device }) => {
  const idx = store.devices.findIndex((d) => d.deviceId === oldId)
  if (idx < 0) {
    return
  }
  if (device.deviceId !== oldId && store.devices.some((d) => d.deviceId === device.deviceId)) {
    store.saveMessage = '设备 ID 已存在，未应用修改'
    queueClearSaveMessage()
    return
  }
  store.devices.splice(idx, 1, device)
  viewer.entities.removeById(oldId)
  entityMap.delete(oldId)
  entityMap.set(device.deviceId, addOneDevice(viewer, device, sceneFrame))
  selectDevice(device.deviceId)
  markDirty([device.deviceId])
}

const handleDeviceRemove = (id) => {
  store.devices = store.devices.filter((d) => d.deviceId !== id)
  viewer.entities.removeById(id)
  entityMap.delete(id)
  store.selectedId = null
}

const queueClearSaveMessage = () => {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    store.saveMessage = ''
  }, 3000)
}

const handleSaveDevices = async () => {
  if (!store.jobId) {
    store.saveMessage = '当前场景不是导入任务，无法保存（请先导入 PCD）'
    queueClearSaveMessage()
    return
  }
  store.saveMessage = '保存中…'
  try {
    await saveDevices(config.apiBase, store.jobId, { devices: store.devices })
    store.saveMessage = '标注已保存'
  } catch (err) {
    store.saveMessage = err?.message || '保存失败，请重试'
  }
  queueClearSaveMessage()
}

onMounted(async () => {
  config = loadConfig()
  viewer = createViewer(viewerEl.value)

  pickHandler = setupPicking(viewer, (id, windowPos) => {
    if (store.annotateMode) {
      handleAnnotateClick(windowPos)
      return
    }
    selectDevice(id)
  })
  window.addEventListener('keydown', onKeydown)

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
  // 自清理：断开 WS、停掉定时器、移除监听、销毁查看器
  clearInterval(panelTimer)
  clearTimeout(saveTimer)
  window.removeEventListener('keydown', onKeydown)
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
      <span v-if="store.saveMessage" class="app-header__msg">{{ store.saveMessage }}</span>
      <button
        class="app-header__btn"
        :class="{ 'is-active': store.annotateMode }"
        type="button"
        @click="toggleAnnotate"
      >
        {{ store.annotateMode ? '退出标注' : '标注设备' }}
      </button>
      <button
        class="app-header__btn"
        type="button"
        :title="store.jobId ? '保存设备标注到 devices.json' : '仅导入任务场景支持保存'"
        :disabled="!store.jobId"
        @click="handleSaveDevices"
      >
        保存标注
      </button>
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
      <div v-if="store.annotateMode" class="annotate-hint">{{ store.annotateHint }}</div>
      <DevicePanel
        v-if="store.selectedId"
        :device-id="store.selectedId"
        class="device-panel"
        @update="handleDeviceUpdate"
        @remove="handleDeviceRemove"
      />
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
  width: 300px;
  max-height: calc(100% - 32px);
  overflow-y: auto;
  z-index: 10;
}

.app-header__spacer {
  flex: 1;
}

.app-header__msg {
  color: var(--color-text-secondary);
  font-size: 12px;
  margin-right: var(--space-3);
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

.app-header__btn:hover:not(:disabled) {
  background: var(--color-accent);
  color: #fff;
}

.app-header__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.app-header__btn.is-active {
  background: var(--color-accent);
  color: #fff;
}

.annotate-hint {
  position: absolute;
  top: var(--space-4);
  left: 50%;
  transform: translateX(-50%);
  background: var(--color-bg-panel);
  border: 1px solid var(--color-warning);
  color: var(--color-text-primary);
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 13px;
  z-index: 10;
}
</style>
