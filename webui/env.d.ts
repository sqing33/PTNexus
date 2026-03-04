/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'

  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>
  export default component
}

interface Window {
  go?: {
    main?: {
      App?: {
        DesktopRequest?: (req: unknown) => Promise<unknown>
        DesktopSSESubscribe?: (url: string) => Promise<unknown>
        DesktopSSEUnsubscribe?: (id: string) => Promise<void>
        OpenDatabaseConfigFile?: () => Promise<void>
        GetDatabaseConfigFilePath?: () => Promise<string>
      }
    }
  }
  runtime?: {
    EventsOn?: (eventName: string, callback: (data: unknown) => void) => void
    EventsOff?: (eventName: string) => unknown
  }
}
