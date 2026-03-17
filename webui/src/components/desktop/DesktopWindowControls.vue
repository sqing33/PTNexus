<template>
  <div
    :class="['desktop-window-controls', { 'desktop-window-controls--compact': compact }]"
    role="group"
    aria-label="窗口控制"
    @dblclick.stop
  >
    <button
      type="button"
      class="desktop-window-control"
      aria-label="最小化窗口"
      title="最小化"
      @click="$emit('minimise')"
    >
      <svg viewBox="0 0 10 10" aria-hidden="true">
        <path d="M1 5.5h8" />
      </svg>
    </button>

    <button
      type="button"
      class="desktop-window-control"
      :aria-label="isMaximised ? '还原窗口' : '最大化窗口'"
      :title="isMaximised ? '还原' : '最大化'"
      @click="$emit('toggle-maximise')"
    >
      <svg v-if="!isMaximised" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M1.5 1.5h7v7h-7z" />
      </svg>
      <svg v-else viewBox="0 0 10 10" aria-hidden="true">
        <path d="M3 1.5h5.5V7" />
        <path d="M1.5 3h5.5v5.5H1.5z" />
      </svg>
    </button>

    <button
      type="button"
      class="desktop-window-control desktop-window-control--close"
      aria-label="关闭窗口"
      title="关闭"
      @click="$emit('close')"
    >
      <svg viewBox="0 0 10 10" aria-hidden="true">
        <path d="M2 2l6 6" />
        <path d="M8 2L2 8" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  compact?: boolean
  isMaximised: boolean
}>()

defineEmits<{
  (e: 'close'): void
  (e: 'minimise'): void
  (e: 'toggle-maximise'): void
}>()
</script>

<style scoped>
.desktop-window-controls {
  --desktop-window-control-width: 46px;
  --desktop-window-control-hover-bg: rgba(25, 35, 52, 0.08);
  --desktop-window-control-icon: #243347;
  --desktop-window-control-close-hover: #d13438;
  --wails-draggable: no-drag;
  display: flex;
  align-items: stretch;
  align-self: stretch;
  height: 100%;
  flex-shrink: 0;
}

.desktop-window-controls--compact {
  --desktop-window-control-width: 44px;
}

.desktop-window-control {
  width: var(--desktop-window-control-width);
  height: 100%;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--desktop-window-control-icon);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition:
    background-color 0.18s ease,
    color 0.18s ease;
}

.desktop-window-control:hover {
  background: var(--desktop-window-control-hover-bg);
}

.desktop-window-control:focus-visible {
  outline: 2px solid rgba(36, 51, 71, 0.3);
  outline-offset: -2px;
}

.desktop-window-control svg {
  width: 10px;
  height: 10px;
  stroke: currentColor;
  stroke-width: 1.2;
  fill: none;
  stroke-linecap: square;
  stroke-linejoin: miter;
}

.desktop-window-control--close:hover,
.desktop-window-control--close:focus-visible {
  background: var(--desktop-window-control-close-hover);
  color: #fff;
  outline: none;
}
</style>
