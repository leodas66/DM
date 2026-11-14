import { buildDownloadPayload, sendToHost } from './types'

const api = (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome

const form = document.getElementById('form') as HTMLFormElement
const urlInput = document.getElementById('url') as HTMLInputElement
const sendBtn = document.getElementById('send') as HTMLButtonElement
const openOptions = document.getElementById('openOptions') as HTMLButtonElement
const status = document.getElementById('status') as HTMLElement
const hostStatus = document.getElementById('hostStatus') as HTMLElement

function setStatus(text: string, ok: boolean) {
  status.textContent = text
  status.className = `status ${ok ? 'ok' : 'err'}`
}

async function checkHost() {
  hostStatus.textContent = 'Checking the DM daemon\u2026'
  const response = await sendToHost({ command: 'ping' })
  if (response.ok) {
    hostStatus.textContent = `DM daemon ready (host ${response.version || 'unknown'}).`
    return
  }
  hostStatus.textContent = response.error || 'The DM daemon is not reachable.'
}

form.addEventListener('submit', async (e) => {
  e.preventDefault()
  const url = urlInput.value.trim()
  if (!url) return

  sendBtn.disabled = true
  setStatus('Sending to DM\u2026', true)

  const item = { url, referrer: '', filename: '', mime: '' } as chrome.downloads.DownloadItem
  const response = await sendToHost(buildDownloadPayload(item))
  sendBtn.disabled = false

  if (response.ok) {
    setStatus('Download added to DM.', true)
    urlInput.value = ''
    return
  }
  setStatus(response.error || 'DM rejected the download.', false)
})

openOptions.addEventListener('click', () => {
  void api.runtime.openOptionsPage()
})

void checkHost()
