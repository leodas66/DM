import {
  buildDownloadPayload,
  daemonBaseUrl,
  isDaemonUrl,
  sendToDaemon,
  shouldCapture,
} from './types'
import { getSettings, saveSettings } from './storage'

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: 'download-with-dm',
    title: 'Download with DM',
    contexts: ['link'],
  })
})

async function configured(): Promise<boolean> {
  const settings = await getSettings()
  return settings.port > 0 && settings.token.length > 0
}

async function handleDownload(item: chrome.downloads.DownloadItem): Promise<void> {
  const settings = await getSettings()
  if (!settings.port || !settings.token) return // not configured: browser handles it

  const decision = shouldCapture(item, settings)
  if (!decision.capture) return

  // The extension captured a download the browser was about to start.
  // Fetch the daemon; only cancel the browser's copy once the daemon accepts.
  const payload = buildDownloadPayload(item, settings)
  try {
    const res = await sendToDaemon(settings, '/api/downloads', payload)
    if (res.ok) {
      try {
        await chrome.downloads.cancel(item.id)
      } catch {
        // cancel failed; the daemon already accepted, so the file will still
        // arrive via the daemon. The browser's copy may finish too — a minor
        // duplicate, acceptable for v1.
      }
    }
  } catch {
    // Daemon unreachable: let the browser download normally.
  }
}

chrome.downloads.onCreated.addListener(handleDownload)

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== 'download-with-dm' || !info.linkUrl) return

  const settings = await getSettings()
  if (!settings.port || !settings.token) return

  try {
    await sendToDaemon(settings, '/api/downloads', {
      url: info.linkUrl,
      referrer: tab?.url || '',
      headers: { 'User-Agent': 'dm/0.1.0-dev' },
      cookies: [],
    })
  } catch {
    // Daemon unreachable: nothing to do, browser won't have started a download
    // from a context menu anyway.
  }
})

// Expose settings to the popup / options via messaging.
chrome.runtime.onMessage.addListener(async (message, _sender, sendResponse) => {
  if (message.type === 'dm:getSettings') {
    sendResponse(await getSettings())
    return true
  }
  if (message.type === 'dm:saveSettings') {
    await saveSettings(message.settings)
    sendResponse({ ok: true })
    return true
  }
  return false
})
