export interface DaemonSettings {
  port: number          // 0 = not configured
  token: string         // bearer token from ~/.config/dm/token
  minSize: number       // bytes; 0 = capture every download
  allowHosts: string[]  // empty = allow every host
  denyHosts: string[]   // empty = deny no host
}

export const DEFAULT_SETTINGS: DaemonSettings = {
  port: 0,
  token: '',
  minSize: 0,
  allowHosts: [],
  denyHosts: [],
}

export interface DownloadRequest {
  url: string
  referrer?: string
  filename?: string
  headers: Record<string, string>
  cookies: Array<{ name: string; value: string }>
}

export interface CaptureDecision {
  capture: boolean
  reason?: string
}

export function daemonBaseUrl(settings: DaemonSettings): string {
  return `http://127.0.0.1:${settings.port}`
}

export function isDaemonUrl(url: string, settings: DaemonSettings): boolean {
  if (!settings.port) return false
  try {
    return url.startsWith(daemonBaseUrl(settings))
  } catch {
    return false
  }
}

export function hostOf(url: string): string | null {
  try {
    return new URL(url).hostname
  } catch {
    return null
  }
}

export function extensionOf(url: string): string {
  try {
    const pathname = new URL(url).pathname
    const last = pathname.split('/').pop() || ''
    const dot = last.lastIndexOf('.')
    if (dot <= 0 || dot === last.length - 1) return ''
    return last.slice(dot + 1).toLowerCase()
  } catch {
    return ''
  }
}

export function mimeOf(item: chrome.downloads.DownloadItem): string {
  return (item.mime || '').toLowerCase()
}

export function shouldCapture(item: chrome.downloads.DownloadItem, settings: DaemonSettings): CaptureDecision {
  // Never capture the daemon's own requests (avoid a loop).
  if (isDaemonUrl(item.url, settings)) {
    return { capture: false, reason: 'daemon url' }
  }

  // Minimum size filter.
  const size = item.fileSize ?? item.totalBytes ?? 0
  if (settings.minSize > 0 && size > 0 && size < settings.minSize) {
    return { capture: false, reason: 'below min size' }
  }

  // Host allow / deny list.
  const host = hostOf(item.url)
  if (host) {
    if (settings.denyHosts.includes(host)) {
      return { capture: false, reason: 'denied host' }
    }
    if (settings.allowHosts.length > 0 && !settings.allowHosts.includes(host)) {
      return { capture: false, reason: 'not in allow list' }
    }
  }

  return { capture: true }
}

export function buildDownloadPayload(item: chrome.downloads.DownloadItem, settings: DaemonSettings): DownloadRequest {
  const headers: Record<string, string> = {}
  const cookies: Array<{ name: string; value: string }> = []

  // User-Agent default.
  headers['User-Agent'] = 'dm/0.1.0-dev'

  const referrer = item.referrer || ''
  if (referrer) headers['Referer'] = referrer

  return {
    url: item.url,
    referrer,
    filename: item.filename || undefined,
    headers,
    cookies,
  }
}

export async function sendToDaemon(
  settings: DaemonSettings,
  path: string,
  body: unknown,
  fetchImpl: typeof fetch = globalThis.fetch,
): Promise<Response> {
  const url = `${daemonBaseUrl(settings)}/api${path}`
  return fetchImpl(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${settings.token}`,
      'Content-Type': 'application/json',
      Origin: `http://127.0.0.1:${settings.port}`,
    },
    body: JSON.stringify(body),
  })
}
