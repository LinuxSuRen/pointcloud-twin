<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { cancelJob, createJob, subscribeJobEvents, getJob } from '../api.js'
import { uploadFile } from '../uploader.js'

const props = defineProps({
  apiBase: { type: String, required: true },
})
const emit = defineEmits(['close', 'completed'])

const phase = ref('form') // form | uploading | converting | done | error
const pcdFile = ref(null)
const devicesFile = ref(null)
const crsEnu = ref('')
const voxel = ref('')
const pointsPerTile = ref('50000')
const refine = ref('add')
const tilesVersion = ref('1.1')
const planeFit = ref(false)

const uploadPercent = ref(0)
const jobView = ref(null)
const errorText = ref('')

let subscription = null
let pollTimer = null

const busy = computed(() => phase.value === 'uploading' || phase.value === 'converting')
const stageText = computed(() => {
  const view = jobView.value
  if (!view) {
    return '任务已提交'
  }
  return view.progress?.message || '处理中'
})
const stagePercent = computed(() => Math.round(jobView.value?.progress?.percent || 0))

const resumeKey = (purpose, file) => `pc2twin-upload-${purpose}-${file.name}-${file.size}`
const loadResumeId = (purpose, file) => {
  try {
    return localStorage.getItem(resumeKey(purpose, file))
  } catch {
    return null
  }
}
const clearResumeId = (purpose, file) => {
  try {
    localStorage.removeItem(resumeKey(purpose, file))
  } catch {
    /* 忽略 */
  }
}

const uploadOne = async (file, purpose) => {
  const id = await uploadFile(file, {
    purpose,
    apiBase: props.apiBase,
    onProgress: ({ uploaded, total }) => {
      uploadPercent.value = total > 0 ? Math.floor((uploaded / total) * 100) : 0
    },
    onUploadId: (uploadId) => {
      try {
        localStorage.setItem(resumeKey(purpose, file), uploadId)
      } catch {
        /* 无 localStorage 时忽略续传 */
      }
    },
    resumeId: loadResumeId(purpose, file),
  })
  clearResumeId(purpose, file)
  return id
}

const submit = async () => {
  if (!pcdFile.value || busy.value) {
    return
  }
  errorText.value = ''
  phase.value = 'uploading'
  uploadPercent.value = 0
  try {
    const pcdUploadId = await uploadOne(pcdFile.value, 'pcd')
    let devicesUploadId = ''
    if (devicesFile.value) {
      uploadPercent.value = 0
      devicesUploadId = await uploadOne(devicesFile.value, 'devices')
    }

    phase.value = 'converting'
    jobView.value = await createJob(props.apiBase, {
      pcdUploadId,
      devicesUploadId,
      options: {
        crsEnu: crsEnu.value.trim(),
        voxel: voxel.value ? Number(voxel.value) : 0,
        pointsPerTile: pointsPerTile.value ? Number(pointsPerTile.value) : 50000,
        refine: refine.value,
        tilesVersion: tilesVersion.value,
        planes: planeFit.value ? 'dist=0.03,min=500' : '',
      },
    })
    watchJob(jobView.value.id)
  } catch (err) {
    errorText.value = err?.message || '导入失败，请重试'
    phase.value = 'error'
  }
}

const watchJob = (id) => {
  subscription = subscribeJobEvents(props.apiBase, id, {
    onEvent: (view) => {
      jobView.value = view
    },
    onEnd: (view) => {
      finish(view)
    },
    onError: () => {
      // SSE 不可用：转 REST 轮询兜底（2 秒）
      startPolling(id)
    },
  })
}

const startPolling = (id) => {
  stopPolling()
  pollTimer = setInterval(async () => {
    try {
      const view = await getJob(props.apiBase, id)
      jobView.value = view
      if (['succeeded', 'failed', 'cancelled'].includes(view.state)) {
        stopPolling()
        finish(view)
      }
    } catch {
      /* 单次查询失败继续轮询 */
    }
  }, 2000)
}

const stopPolling = () => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

const finish = (view) => {
  if (view.state === 'succeeded') {
    phase.value = 'done'
    stopPolling()
    emit('completed', view.result)
    setTimeout(() => emit('close'), 600)
  } else if (view.state === 'cancelled') {
    phase.value = 'form'
    jobView.value = null
    stopPolling()
  } else {
    errorText.value = view.error?.message || '转换失败，请检查输入文件'
    phase.value = 'error'
    stopPolling()
  }
}

const cancel = async () => {
  if (jobView.value?.id) {
    try {
      await cancelJob(props.apiBase, jobView.value.id)
    } catch {
      /* 终态任务取消是 no-op */
    }
  } else {
    emit('close')
  }
}

const close = () => {
  if (!busy.value) {
    emit('close')
  }
}

onUnmounted(() => {
  if (subscription) {
    subscription.close()
  }
  stopPolling()
})
</script>

<template>
  <div class="dialog-mask" @click.self="close">
    <div class="dialog">
      <div class="dialog__head">
        <span class="dialog__title">导入 PCD 点云</span>
        <button class="dialog__close" type="button" :disabled="busy" @click="close">关闭</button>
      </div>

      <template v-if="phase === 'form' || phase === 'error'">
        <div class="field">
          <label class="field__label">PCD 文件</label>
          <input type="file" accept=".pcd" @change="pcdFile = $event.target.files?.[0] || null" />
        </div>
        <div class="field">
          <label class="field__label">设备代理体配置（可选，JSON）</label>
          <input type="file" accept=".json" @change="devicesFile = $event.target.files?.[0] || null" />
        </div>
        <div class="field">
          <label class="field__label">ENU 原点（可选，格式 lon,lat,h）</label>
          <input v-model="crsEnu" placeholder="例如 121.47,31.23,10" />
        </div>
        <div class="field-row">
          <div class="field">
            <label class="field__label">降采样体素（米，可选）</label>
            <input v-model="voxel" placeholder="0.05" />
          </div>
          <div class="field">
            <label class="field__label">单瓦片点数</label>
            <input v-model="pointsPerTile" />
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label class="field__label">LOD 模式</label>
            <select v-model="refine">
              <option value="add">add（不跨层重复）</option>
              <option value="replace">replace</option>
            </select>
          </div>
          <div class="field">
            <label class="field__label">3D Tiles 版本</label>
            <select v-model="tilesVersion">
              <option value="1.1">1.1（glb）</option>
              <option value="1.0">1.0（pnts）</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label class="field__label field__label--inline">
            <input v-model="planeFit" type="checkbox" />
            平面拟合分割（大平面重建为面片，观感更实体）
          </label>
        </div>
        <p v-if="errorText" class="dialog__error">{{ errorText }}</p>
        <div class="dialog__actions">
          <button class="btn btn--primary" type="button" :disabled="!pcdFile" @click="submit">
            上传并转换
          </button>
        </div>
      </template>

      <template v-else-if="phase === 'uploading'">
        <p class="progress-line">正在上传 {{ pcdFile?.name }}</p>
        <div class="progress"><div class="progress__bar" :style="{ width: uploadPercent + '%' }"></div></div>
        <div class="dialog__actions">
          <button class="btn" type="button" @click="cancel">取消</button>
        </div>
      </template>

      <template v-else-if="phase === 'converting'">
        <p class="progress-line">{{ stageText }}</p>
        <div class="progress"><div class="progress__bar" :style="{ width: stagePercent + '%' }"></div></div>
        <div class="dialog__actions">
          <button class="btn" type="button" @click="cancel">取消任务</button>
        </div>
      </template>

      <template v-else>
        <p class="progress-line">转换完成，正在切换场景…</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.dialog-mask {
  position: fixed;
  inset: 0;
  background: rgba(5, 8, 12, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}

.dialog {
  width: 420px;
  max-height: 80vh;
  overflow-y: auto;
  background: var(--color-bg-panel);
  border: 1px solid var(--color-border-subtle);
  border-radius: 10px;
  padding: var(--space-4);
}

.dialog__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-3);
}

.dialog__title {
  color: var(--color-text-bright);
  font-weight: 600;
}

.dialog__close {
  background: none;
  border: none;
  color: var(--color-text-dim);
  cursor: pointer;
  font-size: 12px;
}

.field {
  margin-bottom: var(--space-3);
  flex: 1;
}

.field-row {
  display: flex;
  gap: var(--space-3);
}

.field__label {
  display: block;
  font-size: 12px;
  color: var(--color-text-dim);
  margin-bottom: var(--space-1);
}

.field input,
.field select {
  width: 100%;
  background: var(--color-bg-elevated);
  border: 1px solid var(--color-border-subtle);
  border-radius: 6px;
  color: var(--color-text-primary);
  padding: 6px 8px;
  font-size: 13px;
  box-sizing: border-box;
}

.dialog__error {
  color: var(--color-danger);
  font-size: 12px;
  margin: 0 0 var(--space-2);
}

.field__label--inline {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  color: var(--color-text-secondary);
}

.field input[type='checkbox'] {
  width: auto;
}

.dialog__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.btn {
  background: var(--color-bg-elevated);
  border: 1px solid var(--color-border-subtle);
  color: var(--color-text-primary);
  border-radius: 6px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 13px;
}

.btn--primary {
  background: var(--color-accent);
  border-color: var(--color-accent);
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.progress-line {
  color: var(--color-text-secondary);
  font-size: 13px;
}

.progress {
  height: 6px;
  border-radius: 3px;
  background: var(--color-bg-elevated);
  overflow: hidden;
  margin: var(--space-3) 0;
}

.progress__bar {
  height: 100%;
  background: var(--color-accent);
  transition: width 0.3s;
}
</style>
