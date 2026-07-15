<template>
  <el-popover placement="bottom-end" :width="200" trigger="click">
    <template #reference>
      <el-button type="info" plain size="small">
        <el-icon><Setting /></el-icon>
        列
      </el-button>
    </template>
    <div class="column-toggle-content">
      <div class="column-toggle-actions">
        <el-button link type="primary" size="small" @click="selectAll">全选</el-button>
        <el-button link type="primary" size="small" @click="resetDefault">重置</el-button>
      </div>
      <el-checkbox-group v-model="localVisible" @change="handleChange">
        <div v-for="col in columns" :key="col.prop" class="column-toggle-item">
          <el-checkbox :label="col.prop">{{ col.label }}</el-checkbox>
        </div>
      </el-checkbox-group>
    </div>
  </el-popover>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Setting } from '@element-plus/icons-vue'

export interface ColumnDef {
  prop: string
  label: string
}

const props = defineProps<{
  columns: ColumnDef[]
  modelValue: string[]
  defaults?: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
}>()

const localVisible = ref<string[]>([...props.modelValue])

watch(
  () => props.modelValue,
  (val) => {
    localVisible.value = [...val]
  },
)

const handleChange = (val: string[]) => {
  emit('update:modelValue', [...val])
}

const selectAll = () => {
  const all = props.columns.map((c) => c.prop)
  localVisible.value = [...all]
  emit('update:modelValue', [...all])
}

const resetDefault = () => {
  const def = props.defaults || props.columns.map((c) => c.prop)
  localVisible.value = [...def]
  emit('update:modelValue', [...def])
}
</script>

<style scoped>
.column-toggle-content {
  padding: 4px 0;
}

.column-toggle-actions {
  display: flex;
  gap: 12px;
  margin-bottom: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.column-toggle-item {
  padding: 2px 0;
}

:deep(.column-toggle-item .el-checkbox) {
  width: 100%;
  margin-right: 0;
}
</style>
