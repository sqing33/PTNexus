import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

export type TaskMonitorStatus = 'running' | 'success' | 'failed'

export type TaskMonitorItem = {
  key: string
  kind: string
  rawId?: string | null
  title: string
  status: TaskMonitorStatus
  message: string
  error: string
  progressText: string
  startedAt: number
  updatedAt: number
  routeTarget?: {
    path: string
    query?: Record<string, string>
  }
}

type UpsertTaskInput = {
  key: string
  kind: string
  rawId?: string | null
  title: string
  status?: TaskMonitorStatus
  message?: string
  error?: string
  progressText?: string
  routeTarget?: {
    path: string
    query?: Record<string, string>
  }
}

const normalizeText = (value: unknown) => (typeof value === 'string' ? value.trim() : '')

export const useTaskMonitorStore = defineStore('taskMonitor', () => {
  const tasks = ref<Record<string, TaskMonitorItem>>({})

  const taskList = computed(() =>
    Object.values(tasks.value).sort((a, b) => b.updatedAt - a.updatedAt),
  )

  const runningTasks = computed(() => taskList.value.filter((task) => task.status === 'running'))
  const failedTasks = computed(() => taskList.value.filter((task) => task.status === 'failed'))
  const runningCount = computed(() => runningTasks.value.length)
  const failedCount = computed(() => failedTasks.value.length)
  const hasAttention = computed(() => runningCount.value > 0 || failedCount.value > 0)

  const upsertTask = (input: UpsertTaskInput) => {
    const now = Date.now()
    const existing = tasks.value[input.key]
    tasks.value[input.key] = {
      key: input.key,
      kind: input.kind,
      rawId: input.rawId ?? existing?.rawId ?? null,
      title: normalizeText(input.title) || existing?.title || '后台任务',
      status: input.status ?? existing?.status ?? 'running',
      message: normalizeText(input.message) || '',
      error: normalizeText(input.error) || '',
      progressText: normalizeText(input.progressText) || '',
      startedAt: existing?.startedAt ?? now,
      updatedAt: now,
      routeTarget: input.routeTarget ?? existing?.routeTarget,
    }
  }

  const markRunning = (input: Omit<UpsertTaskInput, 'status'>) => {
    upsertTask({ ...input, status: 'running', error: '' })
  }

  const markSuccess = (key: string, patch: Partial<Omit<UpsertTaskInput, 'key' | 'status'>> = {}) => {
    const existing = tasks.value[key]
    if (!existing && !patch.title) return
    upsertTask({
      key,
      kind: patch.kind ?? existing?.kind ?? 'generic',
      title: patch.title ?? existing?.title ?? '后台任务',
      rawId: patch.rawId ?? existing?.rawId,
      routeTarget: patch.routeTarget ?? existing?.routeTarget,
      message: patch.message ?? existing?.message ?? '',
      progressText: patch.progressText ?? existing?.progressText ?? '',
      error: '',
      status: 'success',
    })
  }

  const markFailed = (key: string, patch: Partial<Omit<UpsertTaskInput, 'key' | 'status'>> = {}) => {
    const existing = tasks.value[key]
    if (!existing && !patch.title) return
    upsertTask({
      key,
      kind: patch.kind ?? existing?.kind ?? 'generic',
      title: patch.title ?? existing?.title ?? '后台任务',
      rawId: patch.rawId ?? existing?.rawId,
      routeTarget: patch.routeTarget ?? existing?.routeTarget,
      message: patch.message ?? existing?.message ?? '',
      progressText: patch.progressText ?? existing?.progressText ?? '',
      error: patch.error ?? existing?.error ?? patch.message ?? '',
      status: 'failed',
    })
  }

  const removeTask = (key: string) => {
    if (!tasks.value[key]) return
    const nextTasks = { ...tasks.value }
    delete nextTasks[key]
    tasks.value = nextTasks
  }

  const clearFinished = () => {
    const nextTasks = Object.fromEntries(
      Object.entries(tasks.value).filter(([, task]) => task.status === 'running'),
    )
    tasks.value = nextTasks
  }

  return {
    tasks,
    taskList,
    runningTasks,
    failedTasks,
    runningCount,
    failedCount,
    hasAttention,
    upsertTask,
    markRunning,
    markSuccess,
    markFailed,
    removeTask,
    clearFinished,
  }
})
