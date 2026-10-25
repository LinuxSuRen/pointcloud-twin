<script setup>
import { computed } from 'vue'
import { store } from '../store.js'
import { alarmText, deviceTypeText, formatPower, formatSoc, formatTemp, formatTime, statusColor } from '../mapping.js'

const props = defineProps({
  deviceId: { type: String, required: true },
})

const device = computed(() => store.devices.find((d) => d.deviceId === props.deviceId))
const metrics = computed(() => store.metrics[props.deviceId])
const color = computed(() => statusColor(metrics.value))

const close = () => {
  store.selectedId = null
}
</script>

<template>
  <div v-if="device" class="panel">
    <div class="panel__head">
      <span class="panel__title">{{ device.deviceId }}</span>
      <button class="panel__close" type="button" @click="close">关闭</button>
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
}

.panel__close:hover {
  color: var(--color-text-secondary);
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
</style>
