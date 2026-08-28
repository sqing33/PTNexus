// src/router/index.ts
import { createRouter, createWebHistory } from 'vue-router'

const whiteList: string[] = ['/login']

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('../views/HomeView.vue'),
    },
    {
      path: '/info',
      name: 'info',
      component: () => import('../views/InfoView.vue'),
    },
    {
      path: '/torrents',
      name: 'torrents',
      component: () => import('../views/TorrentsView.vue'),
    },
    {
      path: '/data',
      name: 'data',
      component: () => import('../views/CrossSeedDataView.vue'),
    },
    {
      path: '/scheduled-seeding',
      name: 'scheduled-seeding',
      component: () => import('../views/ScheduledSeedingView.vue'),
    },
    {
      path: '/auto-seed',
      name: 'auto-seed',
      component: () => import('../views/AutoSeedView.vue'),
    },
    {
      path: '/publish-logs',
      name: 'publish-logs',
      component: () => import('../views/PublishLogsView.vue'),
    },
    {
      path: '/sites',
      name: 'sites',
      component: () => import('../views/SitesView.vue'),
    },
    {
      path: '/resource-info',
      name: 'resource-info',
      component: () => import('../views/ResourceInfoView.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
      redirect: '/settings/general',
      children: [
        {
          path: 'general',
          name: 'settings-general',
          component: () => import('../components/settings/GeneralSettings.vue'),
        },
        {
          path: 'downloader',
          name: 'settings-downloader',
          component: () => import('../components/settings/DownloaderSettings.vue'),
        },
        {
          path: 'cookie',
          name: 'settings-cookie',
          component: () => import('../components/settings/SitesSettings.vue'),
        },
      ],
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
    },
  ],
})

const dynamicImportErrorPattern =
  /loading dynamically imported module|failed to fetch dynamically imported module|importing a module script failed/i

router.onError((error, to) => {
  const message = error instanceof Error ? error.message : String(error || '')
  if (!dynamicImportErrorPattern.test(message)) {
    return
  }

  const reloadKey = `ptnexus:route-reload:${to.fullPath}`
  if (sessionStorage.getItem(reloadKey) === '1') {
    sessionStorage.removeItem(reloadKey)
    return
  }

  sessionStorage.setItem(reloadKey, '1')
  const url = new URL(window.location.href)
  url.searchParams.set('_reload', Date.now().toString())
  window.location.replace(url.toString())
})

// 简单路由守卫：当开启后端认证时，未携带 token 的请求会被 401 拦截
router.beforeEach(async (to, _from, next) => {
  const token = localStorage.getItem('token')
  if (whiteList.includes(to.path)) return next()
  if (!token) {
    // 未登录：直接去 login
    return next({ path: '/login', query: { redirect: to.fullPath } })
  }
  return next()
})

export default router
