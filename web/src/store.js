import { reactive } from 'vue'

// 模块级响应式 store：设备清单、实时指标、连接状态与选中项。
export const store = reactive({
  /** @type {Array<{deviceId:string,deviceType:string,center:number[],size:number[],heading:number}>} */
  devices: [],
  /** deviceId → 最近一帧指标 */
  metrics: {},
  /** WebSocket 连接状态：connecting | open | reconnecting | closed */
  wsState: 'connecting',
  /** 3D Tiles 底图加载状态：loading | ready | error */
  tilesState: 'loading',
  tilesError: '',
  devicesError: '',
  selectedId: null,
  /** 当前场景来源任务的 ID（可保存标注）；非任务场景为 null */
  jobId: null,
  /** 标注模式：在点云上两点画盒 */
  annotateMode: false,
  /** 标注步骤提示 */
  annotateHint: '',
  /** 保存标注的结果提示 */
  saveMessage: '',
})

// applyStatusFrame 应用一帧 deviceId → 指标 数据（增量合并）。
export function applyStatusFrame(data) {
  if (!data || typeof data !== 'object') {
    return
  }
  for (const [id, metrics] of Object.entries(data)) {
    if (!metrics || typeof metrics !== 'object') {
      continue
    }
    store.metrics[id] = { ...store.metrics[id], ...metrics }
  }
}

export function selectDevice(id) {
  store.selectedId = id
}
