export type DesktopRequest = {
  method?: string
  url: string
  headers?: Record<string, string>
  bodyText?: string
  bodyBase64?: string
  timeoutMs?: number
}

export type DesktopResponse = {
  status: number
  statusText?: string
  headers?: Record<string, string>
  bodyBase64?: string
}

export type DesktopSSESubscription = {
  id: string
  eventName: string
}

export type WailsEnvironmentInfo = {
  buildType: string
  platform: string
  arch: string
}

export interface DesktopBridge {
  DesktopRequest: (req: DesktopRequest) => Promise<DesktopResponse>
  DesktopSSESubscribe: (url: string) => Promise<DesktopSSESubscription>
  DesktopSSEUnsubscribe: (id: string) => Promise<void>
  LaunchInstaller?: (path: string) => Promise<void>
  OpenExternalURL?: (url: string) => Promise<void>
  OpenPath?: (path: string) => Promise<void>
  RevealPath?: (path: string) => Promise<void>
}

export interface WailsRuntime {
  EventsOn: (eventName: string, callback: (data: unknown) => void) => void
  EventsOff: (eventName: string) => unknown
  Environment?: () => Promise<WailsEnvironmentInfo>
  WindowHide?: () => void
  WindowIsMaximised?: () => Promise<boolean>
  WindowMinimise?: () => void
  WindowToggleMaximise?: () => void
}

type DesktopRuntimeWindow = Window & {
  go?: { main?: { App?: Partial<DesktopBridge> } }
  runtime?: Partial<WailsRuntime>
}

export const getDesktopBridge = (): DesktopBridge | null => {
  const bridge = (window as DesktopRuntimeWindow).go?.main?.App
  if (!bridge) return null
  if (typeof bridge.DesktopRequest !== 'function') return null
  if (typeof bridge.DesktopSSESubscribe !== 'function') return null
  if (typeof bridge.DesktopSSEUnsubscribe !== 'function') return null
  return bridge as DesktopBridge
}

export const getWailsRuntime = (): WailsRuntime | null => {
  const runtime = (window as DesktopRuntimeWindow).runtime
  if (!runtime) return null
  if (typeof runtime.EventsOn !== 'function') return null
  if (typeof runtime.EventsOff !== 'function') return null
  return runtime as WailsRuntime
}

export const isDesktopRuntime = (): boolean =>
  getDesktopBridge() !== null && getWailsRuntime() !== null
