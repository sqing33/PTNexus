import { onMounted, onUnmounted, ref } from 'vue'
import { getWailsRuntime, isDesktopRuntime, type WailsRuntime } from './bridge'

const MAXIMISE_STATE_SYNC_DELAY_MS = 120

const getDesktopRuntime = (): WailsRuntime | null => {
  if (!isDesktopRuntime()) {
    return null
  }

  return getWailsRuntime()
}

const getWindowRuntime = (): WailsRuntime | null => {
  const runtime = getDesktopRuntime()
  if (!runtime) {
    return null
  }

  if (typeof runtime.WindowMinimise !== 'function') return null
  if (typeof runtime.WindowToggleMaximise !== 'function') return null
  if (typeof runtime.WindowIsMaximised !== 'function') return null
  if (typeof runtime.WindowHide !== 'function') return null

  return runtime
}

export const useDesktopWindowControls = () => {
  const isDesktopShell = ref(isDesktopRuntime())
  const isWindowsDesktop = ref(false)
  const isMaximised = ref(false)

  const syncMaximisedState = async () => {
    const runtime = getWindowRuntime()
    if (!runtime?.WindowIsMaximised) {
      isMaximised.value = false
      return
    }

    try {
      isMaximised.value = await runtime.WindowIsMaximised()
    } catch (error) {
      console.error('[desktop] sync maximised state failed:', error)
      isMaximised.value = false
    }
  }

  const detectWindowsDesktop = async () => {
    const runtime = getDesktopRuntime()
    isDesktopShell.value = runtime !== null
    if (!runtime) {
      isWindowsDesktop.value = false
      return
    }

    if (typeof runtime.Environment !== 'function') {
      isWindowsDesktop.value = true
      await syncMaximisedState()
      return
    }

    try {
      const environment = await runtime.Environment()
      isWindowsDesktop.value = environment.platform === 'windows'
    } catch (error) {
      console.error('[desktop] detect environment failed:', error)
      isWindowsDesktop.value = true
    }

    if (isWindowsDesktop.value) {
      await syncMaximisedState()
    }
  }

  const minimiseWindow = () => {
    if (!isWindowsDesktop.value) {
      return
    }

    getWindowRuntime()?.WindowMinimise?.()
  }

  const toggleWindowMaximise = async () => {
    if (!isWindowsDesktop.value) {
      return
    }

    getWindowRuntime()?.WindowToggleMaximise?.()
    window.setTimeout(() => {
      void syncMaximisedState()
    }, MAXIMISE_STATE_SYNC_DELAY_MS)
  }

  const hideWindowToTray = () => {
    if (!isWindowsDesktop.value) {
      return
    }

    getWindowRuntime()?.WindowHide?.()
  }

  const handleViewportChange = () => {
    if (!isWindowsDesktop.value) {
      return
    }

    void syncMaximisedState()
  }

  onMounted(() => {
    void detectWindowsDesktop()
    window.addEventListener('focus', handleViewportChange)
    window.addEventListener('resize', handleViewportChange)
  })

  onUnmounted(() => {
    window.removeEventListener('focus', handleViewportChange)
    window.removeEventListener('resize', handleViewportChange)
  })

  return {
    isDesktopShell,
    hideWindowToTray,
    isMaximised,
    isWindowsDesktop,
    minimiseWindow,
    syncMaximisedState,
    toggleWindowMaximise,
  }
}
