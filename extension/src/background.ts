import {
  buildDownloadPayload,
  collectCookies,
  ext,
  sendToHost,
  shouldCapture,
  type DownloadRequest,
} from './types'
import { getSettings } from './storage'

const api = ext()

api.runtime.onInstalled.addListener(() => {
  api.contextMenus.create({
    id: 'download-with-dm',
    title: 'Download with DM',
    contexts: ['link'],
  })
})

let badgeTimer: ReturnType<typeof setTimeout> | undefined

// setBadge reports a failure without stealing focus: the browser download was
// left alone, so the user needs to know why DM did not take it.
function setBadge(text: string, title: string) {
  void api.action.setBadgeText({ text })
  void api.action.setTitle({ title })
  if (badgeTimer) clearTimeout(badgeTimer)
  if (!text) return
  badgeTimer = setTimeout(() => {
    void api.action.setBadgeText({ text: '' })
    void api.action.setTitle({ title: 'DM' })
  }, 8000)
}

async function send(payload: DownloadRequest): Promise<boolean> {
  const response = await sendToHost(payload)
  if (!response.ok) {
    setBadge('!', response.error || 'DM could not add the download.')
    return false
  }
  setBadge('', 'DM')
  return true
}

async function handleDownload(item: chrome.downloads.DownloadItem): Promise<void> {
  const settings = await getSettings()
  if (!settings.enabled) return
  if (!shouldCapture(item, settings).capture) return

  const cookies = await collectCookies(item.url)
  const accepted = await send(buildDownloadPayload(item, cookies))
  if (!accepted) return

  // The desktop has displayed the review dialog; stop the browser's copy so
  // it doesn't race the download the user may confirm in DM. If DM couldn't
  // display the dialog, leave the browser's download untouched.
  try {
    await api.downloads.cancel(item.id)
  } catch {
    // Ignore: the daemon already accepted it.
  }
}

api.downloads.onCreated.addListener((item) => {
  void handleDownload(item)
})

api.contextMenus.onClicked.addListener((info, tab) => {
  if (info.menuItemId !== 'download-with-dm' || !info.linkUrl) return
  const item = {
    url: info.linkUrl,
    referrer: tab?.url || '',
    filename: '',
    mime: '',
    fileSize: 0,
    totalBytes: 0,
  } as chrome.downloads.DownloadItem
  void (async () => {
    const cookies = await collectCookies(info.linkUrl!)
    await send(buildDownloadPayload(item, cookies))
  })()
})

// The popup and options page ask the background worker for settings and for a
// host health check, so they never need their own protocol code.
api.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  const type = (message as { type?: string })?.type
  if (type === 'dm:getSettings') {
    void getSettings().then(sendResponse)
    return true
  }
  if (type === 'dm:ping') {
    void sendToHost({ command: 'ping' }).then(sendResponse)
    return true
  }
  return false
})
