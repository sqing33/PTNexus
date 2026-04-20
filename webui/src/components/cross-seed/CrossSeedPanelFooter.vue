<template>
  <footer class="panel-footer">
    <!-- 步骤 0 的按钮 -->
    <div v-if="activeStep === 0" class="button-group">
      <transition name="el-fade-in-linear">
        <div v-if="showCompleteButton" class="check-hint">
          {{ maintenanceOnly ? '确认无误后可直接完成维护。' : '无异常可直接完成，也可先预览。' }}
        </div>
      </transition>
      <el-button @click="handleCancelClick">取消</el-button>

      <el-button
        v-if="showCompleteButton"
        type="primary"
        @click="handleCompleteClick"
        :disabled="isLoading || isNextButtonDisabled"
      >
        修改完成
      </el-button>

      <el-button
        v-if="!maintenanceOnly"
        type="primary"
        @click="goToPublishPreviewStep"
        :disabled="isNextButtonDisabled"
      >
        下一步：发布参数预览
      </el-button>

      <transition name="el-fade-in-linear">
        <div v-if="!maintenanceOnly && isNextButtonDisabled" class="validation-hint">
          <el-icon class="hint-icon">
            <Warning />
          </el-icon>
          <span>{{ nextButtonTooltipContent }}</span>
        </div>
      </transition>
    </div>

    <!-- 步骤 1 的按钮 -->
    <div v-if="!maintenanceOnly && activeStep === 1" class="button-group">
      <el-button @click="handlePreviousStep" :disabled="isLoading">上一步</el-button>

      <el-button
        type="primary"
        @click="handleCompleteClick"
        v-if="showCompleteButton"
        :disabled="isLoading || (isNextButtonDisabled && !isScrolledToBottom)"
      >
        修改完成
      </el-button>

      <el-button type="primary" @click="handleScrollOrNextStep" :disabled="isLoading">
        {{ isNextButtonDisabled && !isScrolledToBottom ? '继续浏览 ↓' : '下一步：选择发布站点' }}
      </el-button>

      <transition name="el-fade-in-linear">
        <div v-if="isNextButtonDisabled && !isScrolledToBottom" class="validation-hint">
          <el-icon class="hint-icon">
            <Warning />
          </el-icon>
          <span>检测到异常，请先浏览完参数信息</span>
        </div>
      </transition>
    </div>

    <!-- 步骤 2 的按钮 -->
    <div v-if="!maintenanceOnly && activeStep === 2" class="button-group">
      <el-button @click="handleCancelClick" :disabled="isLoading">取消</el-button>
      <el-button
        type="warning"
        @click="handleEnqueue"
        :loading="isEnqueueing"
        :disabled="isLoading || selectedTargetSites.length === 0"
      >
        加入队列
      </el-button>
      <el-button
        type="primary"
        @click="handlePublish"
        :loading="isLoading"
        :disabled="isEnqueueing || selectedTargetSites.length === 0"
      >
        立即发布
      </el-button>
    </div>

    <!-- 步骤 3 的按钮 -->
    <div v-if="!maintenanceOnly && activeStep === 3" class="button-group">
      <el-button type="primary" @click="handleCompleteClick">完成</el-button>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { Warning } from '@element-plus/icons-vue'
import { useCrossSeedPanelContext } from './crossSeedPanelContext'

const {
  activeStep,
  maintenanceOnly,
  showCompleteButton,
  isLoading,
  isEnqueueing,
  selectedTargetSites,
  isNextButtonDisabled,
  nextButtonTooltipContent,
  isScrolledToBottom,
  handleCancelClick,
  goToPublishPreviewStep,
  handlePreviousStep,
  handleCompleteClick,
  handleScrollOrNextStep,
  handleEnqueue,
  handlePublish,
} = useCrossSeedPanelContext()
</script>
