<template>
  <el-dialog
    :model-value="visible"
    :title="isEdit ? '编辑任务' : '新建定时发种任务'"
    width="750px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:visible', $event)"
    @open="handleOpen"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="formRules"
      label-width="100px"
      label-position="right"
    >
      <el-form-item v-if="isEdit" label="任务名称" prop="name">
        <el-input v-model="form.name" placeholder="请输入任务名称" clearable />
      </el-form-item>

      <!-- 种子选择：按钮触发弹窗 -->
      <el-form-item label="种子选择" prop="seeds">
        <div class="seed-select-area">
          <el-button type="primary" plain @click="seedDialogVisible = true">
            选择种子
            <el-tag v-if="form.seeds.length > 0" type="success" size="small" style="margin-left: 6px">
              {{ form.seeds.length }}
            </el-tag>
          </el-button>
          <span v-if="form.seeds.length > 0" class="seed-summary">
            已选 {{ form.seeds.length }} 个种子
          </span>
        </div>
        <!-- 已选种子预览列表 -->
        <div v-if="form.seeds.length > 0" class="selected-seeds-preview">
          <div
            v-for="(seed, index) in form.seeds"
            :key="`sel-${seed.site_name}-${seed.torrent_id}`"
            class="selected-seed-item"
          >
            <div class="selected-seed-info">
              <el-tag size="small" effect="plain">{{ seed.site_name }}</el-tag>
              <span class="selected-seed-title" :title="seed.title">{{ seed.title }}</span>
            </div>
            <el-button size="small" type="danger" text @click="removeSeed(index)">移除</el-button>
          </div>
        </div>
      </el-form-item>

      <!-- 目标站点：按钮网格选择 -->
      <el-form-item label="目标站点" prop="target_sites">
        <div class="site-selection-area">
          <div class="site-selection-toolbar">
            <el-button-group>
              <el-button size="small" type="primary" @click="selectAllSites">全选</el-button>
              <el-button size="small" type="info" @click="clearAllSites">清空</el-button>
            </el-button-group>
          </div>
          <div class="site-buttons-group" v-loading="sitesLoading">
            <el-button
              v-for="site in siteList"
              :key="site.name"
              class="site-button"
              :type="form.target_sites.includes(site.name) ? 'success' : 'default'"
              :disabled="site.can_publish === false"
              @click="toggleSite(site.name)"
            >
              {{ site.name }}
            </el-button>
            <div v-if="siteList.length === 0 && !sitesLoading" class="site-empty">
              暂无可用目标站点
            </div>
          </div>
        </div>
      </el-form-item>

      <el-form-item label="发种间隔" prop="interval_minutes">
        <div class="interval-input-group">
          <el-input-number
            v-model="intervalValue"
            :min="1"
            :max="9999"
            controls-position="right"
            style="width: 160px"
          />
          <el-select v-model="intervalUnit" style="width: 100px; margin-left: 8px">
            <el-option label="分钟" value="minutes" />
            <el-option label="小时" value="hours" />
          </el-select>
        </div>
      </el-form-item>

      <el-form-item label="开始时间">
        <el-date-picker
          v-model="form.start_time"
          type="datetime"
          placeholder="留空则立即开始"
          format="YYYY-MM-DD HH:mm"
          value-format="YYYY-MM-DD HH:mm:ss"
          style="width: 220px"
        />
        <span class="form-hint">留空则以当前时间作为发种开始时间</span>
      </el-form-item>

      <el-form-item label="循环发种">
        <el-switch v-model="form.loop_enabled" />
        <span class="form-hint">开启后，所有种子发布完毕将自动从头开始</span>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        {{ isEdit ? '保存' : '创建' }}
      </el-button>
    </template>
  </el-dialog>

  <!-- 种子选择弹窗 -->
  <SeedSelectDialog
    v-model:visible="seedDialogVisible"
    :initial-selection="form.seeds"
    @confirm="handleSeedConfirm"
  />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import axios from 'axios'
import { ElMessage } from '@/utils/uiNotify'
import SeedSelectDialog from './SeedSelectDialog.vue'

type SeedItem = {
  torrent_id: string
  site_name: string
  title: string
}

type TaskData = {
  id: number
  name: string
  status: string
  seeds_json: string
  target_sites_json: string
  interval_minutes: number
  loop_enabled: boolean
  [key: string]: unknown
}

const props = defineProps<{
  visible: boolean
  task: TaskData | null
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  saved: []
}>()

const isEdit = computed(() => !!props.task)

const formRef = ref<FormInstance | null>(null)

type FormModel = {
  name: string
  seeds: SeedItem[]
  target_sites: string[]
  interval_minutes: number
  loop_enabled: boolean
  start_time: string | null
}

const form = reactive<FormModel>({
  name: '',
  seeds: [],
  target_sites: [],
  interval_minutes: 30,
  loop_enabled: false,
  start_time: null,
})

const intervalValue = ref(30)
const intervalUnit = ref('minutes')

watch([intervalValue, intervalUnit], () => {
  form.interval_minutes = intervalUnit.value === 'hours'
    ? intervalValue.value * 60
    : intervalValue.value
})

const generateTimeBasedName = () => {
  const now = new Date()
  const pad = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
}

const formRules: FormRules = {
  seeds: [
    {
      validator: (_rule, _value, callback) => {
        if (form.seeds.length === 0) {
          callback(new Error('请至少选择一个种子'))
        } else {
          callback()
        }
      },
      trigger: 'change',
    },
  ],
  target_sites: [
    {
      validator: (_rule, _value, callback) => {
        if (form.target_sites.length === 0) {
          callback(new Error('请至少选择一个目标站点'))
        } else {
          callback()
        }
      },
      trigger: 'change',
    },
  ],
  interval_minutes: [
    {
      validator: (_rule, _value, callback) => {
        if (form.interval_minutes < 1) {
          callback(new Error('发种间隔不能小于 1 分钟'))
        } else {
          callback()
        }
      },
      trigger: 'change',
    },
  ],
}

// ---- 种子选择弹窗 ----
const seedDialogVisible = ref(false)

const handleSeedConfirm = (seeds: SeedItem[]) => {
  form.seeds = seeds
}

const removeSeed = (index: number) => {
  form.seeds.splice(index, 1)
}

// ---- 目标站点按钮网格 ----
type SiteItem = { name: string; can_publish: boolean }
const siteList = ref<SiteItem[]>([])
const sitesLoading = ref(false)

const toggleSite = (site: string) => {
  const idx = form.target_sites.indexOf(site)
  if (idx > -1) {
    form.target_sites.splice(idx, 1)
  } else {
    form.target_sites.push(site)
  }
}

const selectAllSites = () => {
  form.target_sites = siteList.value.filter((s) => s.can_publish !== false).map((s) => s.name)
}

const clearAllSites = () => {
  form.target_sites = []
}

const fetchSiteList = async () => {
  sitesLoading.value = true
  try {
    const [listRes, statusRes] = await Promise.all([
      axios.get('/api/sites_list'),
      axios.get('/api/sites/status'),
    ])
    // 构建 can_publish 映射 (name → boolean)
    const canPublishMap = new Map<string, boolean>()
    if (Array.isArray(statusRes.data)) {
      for (const s of statusRes.data) {
        canPublishMap.set(s.name, s.can_publish !== false)
      }
    }
    // /api/sites_list 返回 { source_sites: [...], target_sites: [...] }
    if (listRes.data?.target_sites && Array.isArray(listRes.data.target_sites)) {
      siteList.value = listRes.data.target_sites
        .map((s: any) => {
          const name = typeof s === 'string' ? s : s.site || s.nickname || ''
          return name ? { name, can_publish: canPublishMap.get(name) !== false } : null
        })
        .filter(Boolean) as SiteItem[]
    } else if (Array.isArray(listRes.data)) {
      siteList.value = listRes.data
        .map((s: any) => {
          const name = typeof s === 'string' ? s : s.name || ''
          return name ? { name, can_publish: canPublishMap.get(name) !== false } : null
        })
        .filter(Boolean) as SiteItem[]
    }
  } catch (e) {
    console.error('获取站点列表失败:', e)
  } finally {
    sitesLoading.value = false
  }
}

// ---- 表单生命周期 ----
const resetForm = () => {
  form.name = generateTimeBasedName()
  form.seeds = []
  form.target_sites = []
  form.interval_minutes = 30
  form.loop_enabled = false
  form.start_time = null
  intervalValue.value = 30
  intervalUnit.value = 'minutes'
}

const populateFormFromTask = (task: TaskData) => {
  form.name = task.name
  try {
    const seeds = JSON.parse(task.seeds_json)
    form.seeds = Array.isArray(seeds) ? seeds : []
  } catch {
    form.seeds = []
  }
  try {
    const sites = JSON.parse(task.target_sites_json)
    form.target_sites = Array.isArray(sites) ? sites : []
  } catch {
    form.target_sites = []
  }
  form.interval_minutes = task.interval_minutes
  form.loop_enabled = task.loop_enabled

  if (task.interval_minutes >= 60 && task.interval_minutes % 60 === 0) {
    intervalValue.value = task.interval_minutes / 60
    intervalUnit.value = 'hours'
  } else {
    intervalValue.value = task.interval_minutes
    intervalUnit.value = 'minutes'
  }
}

const handleOpen = () => {
  resetForm()
  if (props.task) {
    populateFormFromTask(props.task)
  }
  fetchSiteList()
}

const submitting = ref(false)

const handleSubmit = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch {
    return
  }

  submitting.value = true
  try {
    const payload = {
      name: form.name,
      seeds: form.seeds.map((s) => ({
        torrent_id: s.torrent_id,
        site_name: s.site_name,
        title: s.title,
      })),
      target_sites: form.target_sites,
      interval_minutes: form.interval_minutes,
      loop_enabled: form.loop_enabled,
      ...(form.start_time ? { start_time: form.start_time } : {}),
    }

    let response
    if (isEdit.value && props.task) {
      response = await axios.put(`/api/scheduled-seed/tasks/${props.task.id}`, payload)
    } else {
      response = await axios.post('/api/scheduled-seed/tasks', payload)
    }

    if (!response.data?.success) {
      throw new Error(response.data?.message || '操作失败')
    }

    ElMessage.success(isEdit.value ? '任务已更新' : '任务已创建')
    emit('update:visible', false)
    emit('saved')
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '操作失败'
    ElMessage.error(message)
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.seed-select-area {
  display: flex;
  align-items: center;
  gap: 12px;
}

.seed-summary {
  font-size: 13px;
  color: #606266;
}

.selected-seeds-preview {
  margin-top: 10px;
  max-height: 180px;
  overflow-y: auto;
  border: 1px solid #ebeef5;
  border-radius: 4px;
}

.selected-seed-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  border-bottom: 1px solid #f0f0f0;
  font-size: 13px;
}

.selected-seed-item:last-child {
  border-bottom: none;
}

.selected-seed-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex: 1;
}

.selected-seed-title {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #303133;
}

/* 站点按钮网格 */
.site-selection-area {
  width: 100%;
}

.site-selection-toolbar {
  margin-bottom: 10px;
}

.site-buttons-group {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  min-height: 40px;
}

.site-button {
  min-width: 100px;
}

.site-empty {
  color: #909399;
  font-size: 13px;
  padding: 12px 0;
}

.interval-input-group {
  display: flex;
  align-items: center;
}

.form-hint {
  margin-left: 12px;
  font-size: 12px;
  color: #909399;
}
</style>
