<script setup>
import { computed } from 'vue'
import { store } from '../store.js'
import { deviceTypeText, formatSoc, statusColor } from '../mapping.js'

const emit = defineEmits(['select'])

const items = computed(() =>
  store.devices.map((d) => ({
    id: d.deviceId,
    typeText: deviceTypeText(d.deviceType),
    soc: formatSoc(store.metrics[d.deviceId]?.soc),
    color: statusColor(store.metrics[d.deviceId]),
  })),
)
</script>

<template>
  <div class="device-list">
    <div class="device-list__title">设备列表</div>
    <div v-if="items.length === 0" class="device-list__empty">
      未加载到设备代理体配置，请先用 pc2twin 生成 devices.json
    </div>
    <button
      v-for="item in items"
      :key="item.id"
      class="device-list__item"
      :class="{ 'is-selected': store.selectedId === item.id }"
      type="button"
      @click="emit('select', item.id)"
    >
      <span class="device-list__dot" :style="{ background: item.color }"></span>
      <span class="device-list__name">{{ item.id }}</span>
      <span class="device-list__meta">{{ item.typeText }} · SOC {{ item.soc }}</span>
    </button>
  </div>
</template>

<style scoped>
.device-list {
  padding: var(--space-3) var(--space-2);
}

.device-list__title {
  font-size: 12px;
  color: var(--color-text-dim);
  padding: 0 var(--space-2) var(--space-2);
}

.device-list__empty {
  color: var(--color-text-dim);
  font-size: 12px;
  padding: var(--space-2);
  line-height: 1.6;
}

.device-list__item {
  display: block;
  width: 100%;
  text-align: left;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 6px;
  padding: var(--space-2);
  cursor: pointer;
  color: var(--color-text-primary);
  font-size: 13px;
}

.device-list__item:hover {
  background: var(--color-bg-elevated);
}

.device-list__item.is-selected {
  background: var(--color-bg-elevated);
  border-color: var(--color-accent);
}

.device-list__dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: var(--space-2);
}

.device-list__name {
  font-family: var(--font-mono);
}

.device-list__meta {
  display: block;
  color: var(--color-text-dim);
  font-size: 12px;
  margin: 2px 0 0 16px;
}
</style>
