<script setup>
import { store } from '../store.js'

const WS_STATE_TEXT = {
  connecting: '数据连接中',
  open: '实时数据已连接',
  reconnecting: '实时数据重连中',
  closed: '实时数据已断开',
}

const TILES_STATE_TEXT = {
  loading: '底图加载中',
  ready: '底图已就绪',
  error: '底图加载失败',
}

const wsClass = (state) => {
  if (state === 'open') {
    return 'is-ok'
  }
  if (state === 'closed' || state === 'reconnecting') {
    return 'is-bad'
  }
  return 'is-mid'
}
</script>

<template>
  <div class="status-bar">
    <span class="status-bar__item" :class="wsClass(store.wsState)">
      {{ WS_STATE_TEXT[store.wsState] ?? '实时数据状态未知' }}
    </span>
    <span
      class="status-bar__item"
      :class="store.tilesState === 'ready' ? 'is-ok' : store.tilesState === 'error' ? 'is-bad' : 'is-mid'"
    >
      {{ TILES_STATE_TEXT[store.tilesState] ?? '底图状态未知' }}
    </span>
    <span v-if="store.tilesError" class="status-bar__hint">{{ store.tilesError }}</span>
    <span v-else-if="store.devicesError" class="status-bar__hint">{{ store.devicesError }}</span>
  </div>
</template>

<style scoped>
.status-bar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: 12px;
}

.status-bar__item::before {
  content: '';
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  vertical-align: middle;
}

.is-ok {
  color: var(--color-text-secondary);
}

.is-ok::before {
  background: var(--color-success);
}

.is-mid {
  color: var(--color-text-secondary);
}

.is-mid::before {
  background: var(--color-warning);
}

.is-bad {
  color: var(--color-text-secondary);
}

.is-bad::before {
  background: var(--color-danger);
}

.status-bar__hint {
  color: var(--color-text-dim);
}
</style>
