<template>
  <!-- 路由薄壳：列表 + 维护弹窗都由 CrossSeedDataView(mode=maintenance) 承担 -->
  <div class="seed-maintenance-view">
    <CrossSeedDataView mode="maintenance" @ready="handleReady" />
  </div>
</template>

<script setup lang="ts">
import CrossSeedDataView from '@/views/CrossSeedDataView.vue'

const emit = defineEmits<{
  (e: 'ready', refreshMethod: () => Promise<void>): void
}>()

const handleReady = (refreshMethod: () => Promise<void>) => {
  emit('ready', refreshMethod)
}
</script>

<style scoped>
.seed-maintenance-view {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.seed-maintenance-view :deep(.cross-seed-data-view) {
  flex: 1;
  min-height: 0;
  height: 100%;
}
</style>