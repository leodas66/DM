// Pure helpers shared by the background worker, popup and options page.
// Nothing here talks to the daemon directly: the extension reaches it only
// through the Native Messaging host (io.github.vishwa.dm).

export interface CaptureSettings {
  enabled: boolean
  minSize: number
  allowHosts: string[]
  denyHosts: string[]
}

export const NATIVE_HOST = 'io.github.vishwa.dm'

export interface DownloadCookie {
  name: string
  value: string
}

export interface DownloadRequest {
  command: 'add'
  url: string
  referrer?: string
  filename?: string
  headers: Record<string, string>
  cookies: DownloadCookie[]
}

export interface HostResponse {
  ok: boolean
  id?: string
  filename?: string
  version?: string
  error?: string
}

export interface CaptureDecision {
  capture: boolean
  reason?: string
}

// sendNativeMessage is injectable so the request/response handling is testable
// without a browser.
export type NativeSend = (host: string, message: object) => Promise<unknown>

export function ext(): typeof chrome {
  const scope = globalThis as unknown as { browser?: typeof chrome }
  return scope.browser ?? chrome
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

// baseNameOf keeps only the last path segment. Chrome reports the absolute
// destination path for a download; the daemon expects a filename, and sending
// a path would be a directory traversal waiting to be sanitized away.
export function baseNameOf(path: string): string {
  const trimmed = path.trim()
  if (!trimmed) return ''
  const parts = trimmed.replace(/\\/g, '/').split('/')
  return parts[parts.length - 1] || ''
}

export function shouldCapture(item: chrome.downloads.DownloadItem, settings: CaptureSettings): CaptureDecision {
  const size = item.fileSize ?? item.totalBytes ?? 0
  if (settings.minSize > 0 && size > 0 && size < settings.minSize) {
    return { capture: false, reason: 'below min size' }
  }

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

// buildDownloadPayload is the message the host forwards to the daemon. It
// carries the browser's own referrer and cookies so protected downloads work.
export function buildDownloadPayload(
  item: chrome.downloads.DownloadItem,
  cookies: DownloadCookie[] = [],
): DownloadRequest {
  const headers: Record<string, string> = { 'User-Agent': 'dm/0.2.1' }
  const referrer = item.referrer || ''
  if (referrer) headers['Referer'] = referrer

  return {
    command: 'add',
    url: item.url,
    referrer,
    filename: baseNameOf(item.filename || '') || undefined,
    headers,
    cookies,
  }
}

export type CookieGetter = (details: { url: string }) => Promise<Array<{ name: string; value: string }>>

// collectCookies reads the cookies the browser would send to this URL. A
// failure is not fatal: the download can still be attempted without them.
export async function collectCookies(
  url: string,
  get: CookieGetter = (details) => ext().cookies.getAll(details),
): Promise<DownloadCookie[]> {
  try {
    const list = await get({ url })
    const out: DownloadCookie[] = []
    for (const c of list) {
      if (c.name && typeof c.value === 'string') out.push({ name: c.name, value: c.value })
    }
    return out
  } catch {
    return []
  }
}

export async function sendToHost(
  message: object,
  send: NativeSend = (host, msg) => ext().runtime.sendNativeMessage(host, msg),
): Promise<HostResponse> {
  try {
    const raw = await send(NATIVE_HOST, message)
    if (!raw || typeof raw !== 'object') {
      return { ok: false, error: 'The DM native host sent an unreadable reply.' }
    }
    const response = raw as HostResponse
    return {
      ok: response.ok === true,
      id: response.id,
      filename: response.filename,
      version: response.version,
      error: response.error,
    }
  } catch (err) {
    return { ok: false, error: describeHostError(err) }
  }
}

// describeHostError turns the browser's protocol error into something a user
// can act on. Chrome and Firefox use different wording for a missing host.
export function describeHostError(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err)
  if (/not found|not registered|No such native application/i.test(text)) {
    return 'The DM native host is not installed. See README.md (Native Messaging setup).'
  }
  return `Could not reach the DM native host: ${text}`
}
