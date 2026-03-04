import { getDesktopBridge, getWailsRuntime, isDesktopRuntime } from './bridge'

type DesktopSSEPayload =
  | { type: 'open' }
  | { type: 'message'; data?: string }
  | { type: 'error'; error?: string }
  | { type: 'close' }

export type EventSourceLike = Pick<EventSource, 'onopen' | 'onmessage' | 'onerror' | 'readyState' | 'close'>

class DesktopEventSource implements EventSourceLike {
  onopen: EventSource['onopen'] = null
  onmessage: EventSource['onmessage'] = null
  onerror: EventSource['onerror'] = null
  readyState = 0

  private closed = false
  private subscriptionId: string | null = null
  private eventName: string | null = null

  constructor(private url: string) {
    void this.init()
  }

  private async init() {
    try {
      const bridge = getDesktopBridge()
      const runtime = getWailsRuntime()
      if (!bridge || !runtime) {
        throw new Error('desktop bridge not ready')
      }

      const subscription = await bridge.DesktopSSESubscribe(this.url)
      if (this.closed) {
        await bridge.DesktopSSEUnsubscribe(subscription.id)
        return
      }

      this.subscriptionId = subscription.id
      this.eventName = subscription.eventName

      runtime.EventsOn(this.eventName, (payload: unknown) => {
        const eventPayload = (payload || {}) as DesktopSSEPayload
        if (this.closed) return
        switch (eventPayload?.type) {
          case 'open':
            this.readyState = 1
            if (this.onopen) {
              this.onopen.call(this as unknown as EventSource, new Event('open'))
            }
            return
          case 'message':
            this.readyState = 1
            if (this.onmessage) {
              const event = new MessageEvent('message', { data: String(eventPayload?.data ?? '') })
              this.onmessage.call(this as unknown as EventSource, event)
            }
            return
          case 'error':
            if (this.onerror) {
              this.onerror.call(this as unknown as EventSource, new Event('error'))
            }
            return
          case 'close':
            this.close()
            return
        }
      })
    } catch {
      this.readyState = 2
      if (this.onerror) {
        this.onerror.call(this as unknown as EventSource, new Event('error'))
      }
    }
  }

  close() {
    if (this.closed) return
    this.closed = true
    this.readyState = 2

    const runtime = getWailsRuntime()
    if (runtime && this.eventName) {
      try {
        runtime.EventsOff(this.eventName)
      } catch (error) {
        console.warn('desktop sse EventsOff failed:', error)
      }
    }

    const bridge = getDesktopBridge()
    if (bridge && this.subscriptionId) {
      void bridge.DesktopSSEUnsubscribe(this.subscriptionId)
    }

    this.subscriptionId = null
    this.eventName = null
  }
}

export const openSSE = (url: string): EventSourceLike => {
  if (!isDesktopRuntime()) {
    return new EventSource(url)
  }
  return new DesktopEventSource(url)
}
