<template>
  <header class="panel-header">
    <div class="custom-steps">
      <div
        v-for="(step, index) in steps"
        :key="index"
        class="custom-step"
        :class="{
          active: index === activeStep,
          completed: index < activeStep,
          last: index === steps.length - 1,
        }"
      >
        <div class="step-icon">
          <el-icon v-if="index < activeStep">
            <CircleCheckFilled />
          </el-icon>
          <span v-else>{{ index + 1 }}</span>
        </div>
        <div class="step-title">{{ step.title }}</div>
        <div class="step-connector" v-if="index < steps.length - 1"></div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { CircleCheckFilled } from '@element-plus/icons-vue'

interface StepItem {
  title: string
}

defineProps<{
  steps: StepItem[]
  activeStep: number
}>()
</script>

