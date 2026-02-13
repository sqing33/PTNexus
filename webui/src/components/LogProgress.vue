<template>
  <div v-if="visible" class="log-progress-overlay">
    <div class="log-progress-container">
      <div class="flow-header">
        <div class="flow-title">抓取流程</div>
        <div class="flow-actions">
          <span v-if="streamConnected" class="connection-badge">SSE 已连接</span>
          <el-button text :icon="Close" @click="handleManualClose" />
        </div>
      </div>

      <div class="steps-wrapper">
        <div class="flow-list">
          <template v-for="(step, index) in flowSteps" :key="`${step.name}-${index}`">
            <div class="flow-node" :class="statusClass(step.status)">
              <div class="node-icon">
                <el-icon v-if="step.status === 'processing'" class="is-loading"><Loading /></el-icon>
                <el-icon v-else-if="step.status === 'success'"><CircleCheck /></el-icon>
                <el-icon v-else-if="step.status === 'error'"><CircleClose /></el-icon>
                <el-icon v-else-if="step.status === 'warning'"><Warning /></el-icon>
                <el-icon v-else-if="step.status === 'info'"><InfoFilled /></el-icon>
                <span v-else>{{ index + 1 }}</span>
              </div>

              <div class="node-body">
                <div class="node-title">{{ step.name }}</div>
                <el-tooltip
                  :content="displayMessage(step)"
                  placement="top"
                  :disabled="!isLongMessage(step)"
                >
                  <div class="node-message">
                    {{ displayMessage(step) }}
                  </div>
                </el-tooltip>
              </div>
            </div>

            <div v-if="step.name === parallelParentStep" class="parallel-block">
              <div class="parallel-row">
                <div
                  v-for="subStep in parallelSteps"
                  :key="subStep.name"
                  class="parallel-node"
                  :class="statusClass(subStep.status)"
                >
                  <div class="parallel-icon">
                    <el-icon v-if="subStep.status === 'processing'" class="is-loading">
                      <Loading />
                    </el-icon>
                    <el-icon v-else-if="subStep.status === 'success'"><CircleCheck /></el-icon>
                    <el-icon v-else-if="subStep.status === 'error'"><CircleClose /></el-icon>
                    <el-icon v-else-if="subStep.status === 'warning'"><Warning /></el-icon>
                    <el-icon v-else-if="subStep.status === 'info'"><InfoFilled /></el-icon>
                    <span v-else>•</span>
                  </div>
                  <div class="parallel-body">
                    <el-tooltip :content="displayMessage(subStep)" placement="top">
                      <div class="parallel-title">{{ subStep.name }}</div>
                    </el-tooltip>
                    <el-tooltip :content="displayMessage(subStep)" placement="top">
                      <div class="parallel-message">{{ displayMessage(subStep) }}</div>
                    </el-tooltip>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="index < flowSteps.length - 1" class="flow-arrow">
              <span class="arrow-line"></span>
              <span class="arrow-head">▼</span>
            </div>
          </template>
        </div>
        <div v-if="hasCollapsedDynamicSteps" class="dynamic-toggle">
          <el-button link size="small" @click="toggleDynamicDetails">
            {{ showDynamicDetails ? '收起其他步骤' : `展开其他步骤 (${dynamicSteps.length})` }}
          </el-button>
        </div>

        <div v-if="isComplete" class="completion-message" :class="completionClass">
          <el-icon class="icon-complete">
            <CircleClose v-if="hasError" />
            <CircleCheck v-else />
          </el-icon>
          <span>{{ completionText }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { CircleCheck, CircleClose, Close, InfoFilled, Loading, Warning } from '@element-plus/icons-vue'

type StepStatus = 'pending' | 'processing' | 'success' | 'error' | 'warning' | 'info'

interface StepState {
  name: string
  message: string
  status: StepStatus
  seen: boolean
  timestamp: string
}

const mainStepOrder = [
  '数据库查询',
  '开始抓取',
  '下载种子',
  '解析种子',
  '抓取修复',
  '写入数据库',
  '完成',
]

const parallelParentStep = '抓取修复'
const parallelStepOrder = ['修复海报', '修复简介', '修复截图', '修复媒体信息']
const autoCloseDelayMs = 1200

const props = defineProps<{
  visible: boolean
  taskId: string
}>()

const emit = defineEmits<{
  (e: 'complete'): void
  (e: 'close'): void
}>()

const mainSteps = ref<StepState[]>([])
const parallelSteps = ref<StepState[]>([])
const dynamicSteps = ref<StepState[]>([])
const isComplete = ref(false)
const streamConnected = ref(false)
const currentTaskId = ref('')
const showDynamicDetails = ref(false)
const pendingWriteDbSuccess = ref(false)
const pendingWriteDbSuccessMessage = ref('数据库写入完成')

let eventSource: EventSource | null = null
let autoCloseTimer: ReturnType<typeof setTimeout> | null = null

const dynamicSummaryStep = computed<StepState | null>(() => {
  if (dynamicSteps.value.length === 0) return null
  if (showDynamicDetails.value || dynamicSteps.value.length <= 1) return null

  const seenSteps = dynamicSteps.value.filter((step) => step.seen)
  const latestSeen = seenSteps[seenSteps.length - 1]
  const statuses = dynamicSteps.value.map((step) => step.status)

  let summaryStatus: StepStatus = 'pending'
  if (statuses.includes('error')) summaryStatus = 'error'
  else if (statuses.includes('processing')) summaryStatus = 'processing'
  else if (statuses.includes('warning')) summaryStatus = 'warning'
  else if (statuses.includes('info')) summaryStatus = 'info'
  else if (seenSteps.length > 0 && statuses.every((status) => status === 'success' || status === 'pending')) {
    summaryStatus = 'success'
  }

  return {
    name: `其他步骤 (${dynamicSteps.value.length})`,
    message: latestSeen?.message || `包含 ${dynamicSteps.value.length} 个额外步骤`,
    status: summaryStatus,
    seen: true,
    timestamp: latestSeen?.timestamp || '',
  }
})

const renderedDynamicSteps = computed(() => {
  if (dynamicSteps.value.length === 0) return []
  if (showDynamicDetails.value || dynamicSteps.value.length <= 1) return dynamicSteps.value
  return dynamicSummaryStep.value ? [dynamicSummaryStep.value] : []
})

const flowSteps = computed(() => [...mainSteps.value, ...renderedDynamicSteps.value])

const hasCollapsedDynamicSteps = computed(() => dynamicSteps.value.length > 1)

const hasError = computed(() => {
  const allSteps = [...mainSteps.value, ...parallelSteps.value, ...dynamicSteps.value]
  return allSteps.some((step) => step.status === 'error')
})

const completionText = computed(() =>
  hasError.value ? '流程结束（存在错误）' : '所有步骤已完成',
)

const completionClass = computed(() =>
  hasError.value ? 'completion-message-error' : 'completion-message-success',
)

const createStepState = (name: string): StepState => ({
  name,
  message: '',
  status: 'pending',
  seen: false,
  timestamp: '',
})

const resetFlowState = () => {
  mainSteps.value = mainStepOrder.map(createStepState)
  parallelSteps.value = parallelStepOrder.map(createStepState)
  dynamicSteps.value = []
  showDynamicDetails.value = false
  pendingWriteDbSuccess.value = false
  pendingWriteDbSuccessMessage.value = '数据库写入完成'
  isComplete.value = false
  streamConnected.value = false
  clearAutoCloseTimer()
}

const clearAutoCloseTimer = () => {
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
}

const normalizeStatus = (value: unknown): StepStatus => {
  const status = String(value || '')
    .trim()
    .toLowerCase()

  if (status === 'processing') return 'processing'
  if (status === 'success') return 'success'
  if (status === 'error') return 'error'
  if (status === 'warning') return 'warning'
  if (status === 'info') return 'info'
  return 'pending'
}

const findStep = (steps: StepState[], name: string): StepState | undefined =>
  steps.find((step) => step.name === name)

const updateStepState = (step: StepState, message: string, status: StepStatus, timestamp: string) => {
  step.seen = true
  step.message = (message || '').trim()
  step.status = status
  if (timestamp) {
    step.timestamp = timestamp
  }
}

const ensureDynamicStep = (name: string): StepState => {
  const existed = findStep(dynamicSteps.value, name)
  if (existed) return existed
  const created = createStepState(name)
  dynamicSteps.value.push(created)
  return created
}

const parallelStepNameSet = new Set(parallelStepOrder)

const markUnseenParallelSteps = (
  timestamp: string,
  status: StepStatus,
  message: string,
  includeProcessing: boolean,
) => {
  for (const step of parallelSteps.value) {
    const shouldPatchPending = !step.seen || step.status === 'pending'
    const shouldPatchProcessing = includeProcessing && step.status === 'processing'
    if (shouldPatchPending || shouldPatchProcessing) {
      updateStepState(step, message, status, timestamp)
    }
  }
}

const updateStep = (stepName: string, message: string, rawStatus: unknown, rawTimestamp: unknown) => {
  const safeMessage = String(message || '')
  let status = normalizeStatus(rawStatus)
  const timestamp = typeof rawTimestamp === 'string' ? rawTimestamp : ''
  const repairStep = findStep(mainSteps.value, parallelParentStep)
  const writeDbStep = findStep(mainSteps.value, '写入数据库')

  // 兼容当前后端语义：数据库未命中会继续抓取，不应视为最终失败。
  if (stepName === '数据库查询' && status === 'error' && safeMessage.includes('未找到缓存')) {
    status = 'warning'
  }

  // 后端仍会发“站点校验”事件，这里并入“开始抓取”步骤展示，不单列节点。
  if (stepName === '站点校验') {
    const startStep = findStep(mainSteps.value, '开始抓取')
    if (startStep) {
      updateStepState(startStep, safeMessage, status, timestamp)
    }
    return
  }

  // 进入抓取后续阶段后，自动将“开始抓取”置为完成。
  if (['下载种子', '解析种子', '抓取修复', '写入数据库', '完成'].includes(stepName)) {
    const startStep = findStep(mainSteps.value, '开始抓取')
    if (startStep && startStep.status !== 'success' && startStep.status !== 'error') {
      const startMessage = startStep.message || '已开始从源站抓取'
      updateStepState(startStep, startMessage, 'success', timestamp)
    }
  }

  if (stepName === '写入数据库' && status === 'success' && repairStep) {
    const repairDone = repairStep.status === 'success' || repairStep.status === 'error'
    if (!repairDone && writeDbStep) {
      pendingWriteDbSuccess.value = true
      pendingWriteDbSuccessMessage.value = safeMessage || '数据库写入完成'
      updateStepState(writeDbStep, '已写入，等待修复收尾...', 'processing', timestamp)
      return
    }
  }

  if (stepName === parallelParentStep) {
    if (status === 'success') {
      markUnseenParallelSteps(timestamp, 'success', '无需修复', false)
      if (pendingWriteDbSuccess.value && writeDbStep) {
        updateStepState(
          writeDbStep,
          pendingWriteDbSuccessMessage.value || '数据库写入完成',
          'success',
          timestamp,
        )
        pendingWriteDbSuccess.value = false
      }
    } else if (status === 'error' && pendingWriteDbSuccess.value && writeDbStep) {
      updateStepState(
        writeDbStep,
        '已写入，但修复阶段存在错误',
        'warning',
        timestamp,
      )
      pendingWriteDbSuccess.value = false
    }
  }

  const mainStep = findStep(mainSteps.value, stepName)
  if (mainStep) {
    updateStepState(mainStep, safeMessage, status, timestamp)
    return
  }

  const parallelStep = findStep(parallelSteps.value, stepName)
  if (parallelStep) {
    const parentStep = findStep(mainSteps.value, parallelParentStep)
    if (parentStep && !parentStep.seen) {
      updateStepState(parentStep, '正在并发处理子任务...', 'processing', timestamp)
    }
    updateStepState(parallelStep, safeMessage, status, timestamp)
    return
  }

  const dynamicStep = ensureDynamicStep(stepName)
  updateStepState(dynamicStep, safeMessage, status, timestamp)
}

const displayMessage = (step: StepState) => {
  if (step.message) return step.message
  if (step.status === 'processing') return '处理中...'
  if (step.status === 'success') return '执行完成'
  if (step.status === 'warning') return '执行完成（有告警）'
  if (step.status === 'error') return '执行失败'
  if (step.status === 'info') return '信息提示'
  if (parallelStepNameSet.has(step.name)) return '待检查'
  return '等待执行...'
}

const isLongMessage = (step: StepState) => displayMessage(step).length > 20

const statusClass = (status: StepStatus) => `status-${status}`

const toggleDynamicDetails = () => {
  showDynamicDetails.value = !showDynamicDetails.value
}

const handleComplete = () => {
  const completionTimestamp = new Date().toISOString()
  markUnseenParallelSteps(completionTimestamp, 'success', '无需修复', false)
  markUnseenParallelSteps(completionTimestamp, 'warning', '未收到结束日志（可能已在后台继续）', true)
  if (pendingWriteDbSuccess.value) {
    const writeDbStep = findStep(mainSteps.value, '写入数据库')
    if (writeDbStep) {
      updateStepState(
        writeDbStep,
        pendingWriteDbSuccessMessage.value || '数据库写入完成',
        'success',
        completionTimestamp,
      )
    }
    pendingWriteDbSuccess.value = false
  }
  isComplete.value = true
  disconnectSSE()

  if (!hasError.value) {
    emit('complete')
    clearAutoCloseTimer()
    autoCloseTimer = setTimeout(() => {
      emit('close')
    }, autoCloseDelayMs)
  }
}

const connectSSE = () => {
  if (!props.taskId) return

  if (
    eventSource &&
    currentTaskId.value === props.taskId &&
    eventSource.readyState !== EventSource.CLOSED
  ) {
    return
  }

  disconnectSSE()
  resetFlowState()
  currentTaskId.value = props.taskId

  eventSource = new EventSource(`/api/migrate/logs/stream/${props.taskId}`)

  eventSource.onopen = () => {
    streamConnected.value = true
  }

  eventSource.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data)
      if (payload.type === 'connected') {
        streamConnected.value = true
        return
      }

      if (payload.type === 'log') {
        updateStep(payload.step, payload.message, payload.status, payload.timestamp)
        return
      }

      if (payload.type === 'complete') {
        handleComplete()
      }
    } catch (error) {
      console.error('解析 SSE 消息失败:', error)
    }
  }

  eventSource.onerror = (error) => {
    console.error('SSE 连接错误:', error)
    disconnectSSE()
  }
}

const disconnectSSE = () => {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
  streamConnected.value = false
}

const handleManualClose = () => {
  disconnectSSE()
  clearAutoCloseTimer()
  emit('close')
}

watch(
  () => [props.visible, props.taskId] as const,
  ([visible, taskId]) => {
    if (!visible) {
      disconnectSSE()
      clearAutoCloseTimer()
      return
    }
    if (!taskId) return
    connectSSE()
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  disconnectSSE()
  clearAutoCloseTimer()
})
</script>

<style scoped>
.log-progress-overlay {
  position: absolute;
  top: 43px;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(255, 255, 255, 0.75);
  backdrop-filter: blur(2px);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
}

.log-progress-container {
  width: 840px;
  max-width: 96%;
  max-height: min(68vh, calc(100% - 8px));
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12);
  overflow: hidden;
  border: 1px solid #ebeef5;
}

.flow-header {
  height: 42px;
  padding: 0 10px 0 12px;
  border-bottom: 1px solid #f0f2f5;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.flow-title {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
}

.flow-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.connection-badge {
  font-size: 11px;
  color: #067647;
  background: #ecfdf3;
  border: 1px solid #a6f4c5;
  border-radius: 999px;
  padding: 1px 6px;
}

.steps-wrapper {
  padding: 8px 10px 8px;
  max-height: calc(min(68vh, calc(100% - 8px)) - 42px);
  overflow: hidden;
}

.flow-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.flow-node {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding: 6px 8px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #fff;
  color: #111827;
}

.node-icon {
  width: 16px;
  min-width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1px solid #cfd4dc;
  color: #6b7280;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
}

.node-body {
  min-width: 0;
  flex: 1;
}

.node-title {
  font-size: 13px;
  font-weight: 600;
  line-height: 16px;
  color: #111827;
}

.node-message {
  margin-top: 1px;
  font-size: 12px;
  line-height: 15px;
  color: #374151;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.flow-arrow {
  margin: 0 0 0 16px;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 10px;
}

.flow-arrow .arrow-line {
  width: 1px;
  flex: 1;
  background: #cdd0d6;
}

.flow-arrow .arrow-head {
  font-size: 9px;
  line-height: 8px;
  color: #909399;
}

.parallel-block {
  margin: 0 0 2px 16px;
  border: 1px dashed #dcdfe6;
  border-radius: 6px;
  padding: 4px 6px;
  background: #fafafa;
}

.parallel-row {
  margin: 4px 0;
  display: grid;
  gap: 4px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.parallel-node {
  display: flex;
  gap: 4px;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  background: #fff;
  padding: 4px;
  color: #111827;
}

.parallel-icon {
  width: 14px;
  min-width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 1px solid #cfd4dc;
  color: #6b7280;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 9px;
}

.parallel-body {
  min-width: 0;
  flex: 1;
}

.parallel-title {
  font-size: 12px;
  font-weight: 600;
  line-height: 14px;
  color: #111827;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.parallel-message {
  margin-top: 2px;
  font-size: 11px;
  line-height: 13px;
  color: #374151;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dynamic-toggle {
  margin-top: 2px;
  text-align: right;
  line-height: 1;
}

.status-pending {
  border-color: #d1d5db;
  background: #f9fafb;
}

.status-processing {
  border-color: #b3d8ff;
  background: #eff6ff;
}

.status-success {
  border-color: #c2e7b0;
  background: #f0fdf4;
}

.status-warning {
  border-color: #f3d19e;
  background: #fffbeb;
}

.status-error {
  border-color: #fab6b6;
  background: #fef2f2;
}

.status-info {
  border-color: #a0cfff;
  background: #f0f9ff;
}

.flow-node.status-pending .node-icon,
.parallel-node.status-pending .parallel-icon {
  color: #6b7280;
}

.flow-node.status-processing .node-icon,
.parallel-node.status-processing .parallel-icon {
  color: #2563eb;
  border-color: #93c5fd;
}

.flow-node.status-success .node-icon,
.parallel-node.status-success .parallel-icon {
  color: #16a34a;
  border-color: #86efac;
}

.flow-node.status-warning .node-icon,
.parallel-node.status-warning .parallel-icon {
  color: #d97706;
  border-color: #fcd34d;
}

.flow-node.status-error .node-icon,
.parallel-node.status-error .parallel-icon {
  color: #dc2626;
  border-color: #fca5a5;
}

.flow-node.status-info .node-icon,
.parallel-node.status-info .parallel-icon {
  color: #0284c7;
  border-color: #7dd3fc;
}

.completion-message {
  margin-top: 4px;
  border-radius: 6px;
  padding: 6px 8px;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-weight: 600;
}

.completion-message-success {
  color: #2e7d32;
  background: linear-gradient(135deg, #e8f5e9 0%, #c8e6c9 100%);
}

.completion-message-error {
  color: #b42318;
  background: linear-gradient(135deg, #fff5f5 0%, #ffe4e4 100%);
}

.icon-complete {
  font-size: 14px;
}

@media (max-width: 1199px) {
  .parallel-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 899px) {
  .parallel-row {
    grid-template-columns: repeat(1, minmax(0, 1fr));
  }
}

@media (max-width: 768px) {
  .log-progress-overlay {
    top: 0;
    padding: 8px;
    box-sizing: border-box;
  }

  .log-progress-container {
    width: 100%;
    max-width: none;
    max-height: calc(100% - 8px);
  }

  .steps-wrapper {
    max-height: calc(100% - 42px);
  }

  .connection-badge {
    display: none;
  }
}
</style>
