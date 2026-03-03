<!-- views/SettingsView.vue -->
<template>
  <el-container class="settings-container" :class="{ 'is-mobile': isMobile }">
    <el-aside :width="isMobile ? '100%' : '200px'" class="settings-aside">
      <el-menu :default-active="route.path" class="settings-menu" :mode="isMobile ? 'horizontal' : 'vertical'" router>
        <el-menu-item index="/settings/general">
          <el-icon>
            <Setting />
          </el-icon>
          <span>设置</span>
        </el-menu-item>
        <el-menu-item index="/settings/downloader">
          <el-icon>
            <Download />
          </el-icon>
          <span>下载器</span>
        </el-menu-item>
        <el-menu-item index="/settings/cookie">
          <el-icon>
            <Tickets />
          </el-icon>
          <span>站点管理</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-main class="settings-main">
      <router-view class="settings-content" />
    </el-main>
  </el-container>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { Download, Setting, Tickets } from '@element-plus/icons-vue'

const route = useRoute()

const isMobile = ref(false)

const updateIsMobile = () => {
  isMobile.value = window.innerWidth <= 768
}

onMounted(() => {
  updateIsMobile()
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateIsMobile)
})
</script>

<style scoped>
.settings-container {
  height: 100%;
}

.settings-aside {
  border-right: 1px solid rgba(220, 223, 230, 0.5);
  background-color: rgba(255, 255, 255, 0.5) !important;
  backdrop-filter: blur(5px);
}

.settings-menu {
  height: 100%;
  border-right: none;
  background-color: transparent !important;
}

.settings-menu :deep(.el-menu-item) {
  background-color: transparent !important;
}

.settings-menu :deep(.el-menu-item:hover) {
  background-color: rgba(0, 0, 0, 0.05) !important;
}

.settings-menu :deep(.el-menu-item.is-active) {
  background-color: rgba(64, 158, 255, 0.1) !important;
}

.settings-main {
  padding: 0;
  display: flex;
  flex-direction: column;
  overflow: auto;
  background-color: transparent;
}

.settings-content {
  flex: 1;
  min-height: 0;
}

@media (max-width: 768px) {
  .settings-container {
    flex-direction: column;
  }

  .settings-aside {
    display: none;
  }

  .settings-menu {
    height: auto;
    overflow-x: auto;
    white-space: nowrap;
  }

  .settings-menu :deep(.el-menu-item) {
    min-width: 110px;
    justify-content: center;
  }

  .settings-main {
    min-height: 0;
  }
}
</style>
