<script setup>
import { computed, reactive, watch } from 'vue'
import { store } from '../store.js'
import {
  alarmText,
  deviceTypeText,
  formatPower,
  formatSoc,
  formatTemp,
  formatTime,
  statusColor,
} from '../mapping.js'

const props = defineProps({
  deviceId: { type: String, required: true },
})
const emit = defineEmits(['update', 'remove'])

const device = computed(() => store.devices.find((d) => d.deviceId === props.deviceId))
const metrics = computed(() => store.metrics[props.deviceId])
const color = computed(() => statusColor(metrics.value))

// 编辑表单的本地副本（选中变化时重置；change 时提交）
const form = reactive({
  deviceId: '',
  deviceType: '',
  cx: 0,
  cy: 0,
  cz: 0,
  dx: 0,
  dy: 0,
  dz: 0,
  heading: 0,
})

watch(
  () => props.deviceId,
  () => resetForm(),
  { immediate: true },
)

function resetForm() {
  const d = device.value
  if (!d) {
    return
  }
  form.deviceId = d.deviceId
  form.deviceType = d.deviceType
  form.cx = d.center[0]
  form.cy = d.center[1]
  form.cz = d.center[2]
  form.dx = d.size[0]
  form.dy = d.size[1]
  form.dz = d.size[2]
  form.heading = d.heading
}

const apply = () => {
  if (!form.deviceId.trim()) {
    store.saveMessage = '设备 ID 不能为空，未应用修改'
    return
  }
  if ([form.dx, form.dy, form.dz].some((v) => !(Number(v) > 0))) {
    store.saveMessage = '包围盒尺寸必须为正数，未应用修改'
    return
  }
  emit('update', {
    oldId: props.deviceId,
    device: {
      deviceId: form.deviceId.trim(),
      deviceType: form.deviceType || 'other',
      center: [Number(form.cx), Number(form.cy), Number(form.cz)],
      size: [Number(form.dx), Number(form.dy), Number(form.dz)],
      heading: Number(form.heading) || 0,
    },
  })
}

const remove = () => {
  emit('remove', props.deviceId)
}

const close = () => {
  store.selectedId = null
}

const num = (v) => Number(v).toFixed(1)
</script>

<template>
  <div v-if="device" class="panel">
    <div class="panel__head">
      <span class="panel__title">{{ device.deviceId }}</span>
      <div>
        <button class="panel__close panel__close--danger" type="button" @click="remove">删除</button>
        <button class="panel__close" type="button" @click="close">关闭</button>
      </div>
    </div>

    <div class="panel__row">
      <span class="panel__label">设备类型</span>
      <span class="panel__value">{{ deviceTypeText(device.deviceType) }}</span>
    </div>
    <div class="panel__row">
      <span class="panel__label">告警状态</span>
      <span class="panel__value" :style="{ color }">{{ alarmText(metrics?.alarmLevel) }}</span>
    </div>
    <div class="panel__row">
      <span class="panel__label">SOC</span>
      <span class="panel__value panel__value--mono">{{ formatSoc(metrics?.soc) }}</span>
    </div>
    <div class="panel__row">
      <span class="panel__label">最高温度</span>
      <span class="panel__value panel__value--mono">{{ formatTemp(metrics?.maxTemp) }}</span>
    </div>
    <div class="panel__row">
      <span class="panel__label">功率</span>
      <span class="panel__value panel__value--mono">{{ formatPower(metrics?.power) }}</span>
    </div>
    <div class="panel__row">
      <span class="panel__label">数据时间</span>
      <span class="panel__value panel__value--mono">{{ formatTime(metrics?.ts) }}</span>
    </div>
    <div v-if="!metrics" class="panel__empty">暂未收到该设备的实时数据</div>

    <div class="panel__edit">
      <div class="panel__edit-title">编辑代理体（修改后失去实时数据关联需 ID 一致）</div>
      <div class="panel__field">
        <label>设备 ID</label>
        <input v-model="form.deviceId" @change="apply" />
      </div>
      <div class="panel__field">
        <label>设备类型</label>
        <select v-model="form.deviceType" @change="apply">
          <option value="battery-cluster">电池舱</option>
          <option value="pcs">PCS 柜</option>
          <option value="transformer">变压器</option>
          <option value="other">其他</option>
        </select>
      </div>
      <div class="panel__field-row">
        <div class="panel__field">
          <label>中心 X</label>
          <input v-model.number="form.cx" type="number" step="0.1" @change="apply" />
        </div>
        <div class="panel__field">
          <label>中心 Y</label>
          <input v-model.number="form.cy" type="number" step="0.1" @change="apply" />
        </div>
        <div class="panel__field">
          <label>中心 Z</label>
          <input v-model.number="form.cz" type="number" step="0.1" @change="apply" />
        </div>
      </div>
      <div class="panel__field-row">
        <div class="panel__field">
          <label>尺寸 X</label>
          <input v-model.number="form.dx" type="number" step="0.1" @change="apply" />
        </div>
        <div class="panel__field">
          <label>尺寸 Y</label>
          <input v-model.number="form.dy" type="number" step="0.1" @change="apply" />
        </div>
        <div class="panel__field">
          <label>尺寸 Z</label>
          <input v-model.number="form.dz" type="number" step="0.1" @change="apply" />
        </div>
      </div>
      <div class="panel__field">
        <label>朝向（度，俯视顺时针）</label>
        <input v-model.number="form.heading" type="number" step="1" @change="apply" />
      </div>
      <div class="panel__hint">当前中心 {{ num(device.center[0]) }}, {{ num(device.center[1]) }}, {{ num(device.center[2]) }} 米</div>
    </div>
  </div>
</template>

<style scoped>
.panel {
  background: var(--color-bg-panel);
  border: 1px solid var(--color-border-subtle);
  border-radius: 8px;
  padding: var(--space-3) var(--space-4);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}

.panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--color-border-subtle);
  margin-bottom: var(--space-2);
}

.panel__title {
  font-family: var(--font-mono);
  color: var(--color-text-bright);
}

.panel__close {
  background: none;
  border: none;
  color: var(--color-text-dim);
  cursor: pointer;
  font-size: 12px;
  margin-left: var(--space-2);
}

.panel__close:hover {
  color: var(--color-text-secondary);
}

.panel__close--danger:hover {
  color: var(--color-danger);
}

.panel__row {
  display: flex;
  justify-content: space-between;
  padding: var(--space-1) 0;
}

.panel__label {
  color: var(--color-text-dim);
}

.panel__value {
  color: var(--color-text-primary);
}

.panel__value--mono {
  font-family: var(--font-mono);
}

.panel__empty {
  margin-top: var(--space-2);
  color: var(--color-text-dim);
  font-size: 12px;
}

.panel__edit {
  margin-top: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--color-border-subtle);
}

.panel__edit-title {
  color: var(--color-text-dim);
  font-size: 12px;
  margin-bottom: var(--space-2);
}

.panel__field {
  flex: 1;
  margin-bottom: var(--space-2);
  min-width: 0;
}

.panel__field label {
  display: block;
  font-size: 11px;
  color: var(--color-text-dim);
  margin-bottom: 2px;
}

.panel__field input,
.panel__field select {
  width: 100%;
  background: var(--color-bg-elevated);
  border: 1px solid var(--color-border-subtle);
  border-radius: 4px;
  color: var(--color-text-primary);
  padding: 4px 6px;
  font-size: 12px;
  box-sizing: border-box;
  font-family: var(--font-mono);
}

.panel__field-row {
  display: flex;
  gap: var(--space-2);
}

.panel__hint {
  color: var(--color-text-dim);
  font-size: 11px;
}
</style>
