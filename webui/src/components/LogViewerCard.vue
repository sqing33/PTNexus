<template>
  <Teleport to="body">
    <div v-if="modelValue" class="log-viewer">
      <div class="log-viewer__overlay" @click="close" />
      <el-card class="log-viewer__card" shadow="xl" @click.stop>
        <template #header>
          <div class="log-viewer__header">
            <span class="log-viewer__title">{{ title }}</span>
            <div class="log-viewer__actions">
              <el-button text :icon="CopyDocument" @click="handleCopy" />
              <el-button text :icon="Download" @click="handleDownload" />
              <el-button text :icon="Close" @click="close" />
            </div>
          </div>
        </template>
        <pre class="log-viewer__pre">{{ content }}</pre>
      </el-card>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Close, CopyDocument, Download } from '@element-plus/icons-vue'

const props = defineProps<{
  modelValue: boolean
  title: string
  content: string
  downloadName?: string
  width?: string
  maxHeight?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'close'): void
}>()

const width = computed(() => props.width || 'min(900px, calc(100vw - 32px))')
const maxHeight = computed(() => props.maxHeight || '80vh')

const safeTitle = computed(() => {
  const raw = String(props.downloadName || props.title || 'logs').trim()
  const cleaned = raw.replace(/[\\/:*?"<>|]/g, '_').replace(/\s+/g, ' ').trim()
  return cleaned || 'logs'
})

const timestamp = () => {
  const now = new Date()
  const yyyy = now.getFullYear()
  const mm = String(now.getMonth() + 1).padStart(2, '0')
  const dd = String(now.getDate()).padStart(2, '0')
  const hh = String(now.getHours()).padStart(2, '0')
  const mi = String(now.getMinutes()).padStart(2, '0')
  const ss = String(now.getSeconds()).padStart(2, '0')
  return `${yyyy}${mm}${dd}-${hh}${mi}${ss}`
}

const close = () => {
  emit('update:modelValue', false)
  emit('close')
}

const handleCopy = async () => {
  const text = String(props.content || '').trim()
  if (!text) return ElMessage.warning('无内容可复制')
  if (!navigator.clipboard?.writeText) return ElMessage.error('当前环境不支持复制')
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : '复制失败'
    ElMessage.error(message)
  }
}

const handleDownload = () => {
  const text = String(props.content || '').trim()
  if (!text) return ElMessage.warning('无内容可下载')

  const fileName = `${safeTitle.value}-${timestamp()}.txt`
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)

  const a = document.createElement('a')
  a.href = url
  a.download = fileName
  a.rel = 'noopener'
  a.click()

  URL.revokeObjectURL(url)
}

const handleKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Escape') close()
}

watch(
  () => props.modelValue,
  (visible) => {
    if (visible) window.addEventListener('keydown', handleKeydown)
    else window.removeEventListener('keydown', handleKeydown)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.log-viewer__overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  z-index: 1999;
}

.log-viewer__card {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: v-bind(width);
  z-index: 2000;
  display: flex;
  flex-direction: column;
  max-height: v-bind(maxHeight);
}

.log-viewer__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.log-viewer__title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-viewer__actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.log-viewer__card :deep(.el-card__body) {
  overflow-y: auto;
  flex: 1;
}

.log-viewer__pre {
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  font-family: 'Courier New', Courier, monospace;
  font-size: 13px;
  line-height: 1.4;
  color: #606266;
}
</style>
