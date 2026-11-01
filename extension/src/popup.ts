import { getSettings, saveSettings } from './storage'
import { sendToDaemon } from './types'

const form = document.getElementById('form') as HTMLFormElement
const urlInput = document.getElementById('url') as HTMLInputElement
const sendBtn = document.getElementById('send') as HTMLButtonElement
const openOptions = document.getElementById('openOptions') as HTMLButtonElement
const status = document.getElementById('status') as HTMLElement

function setStatus(text: string, ok: boolean) {
  status.textContent = text
  status.className = `status ${ok ? 'ok' : 'err'}`
}

form.addEventListener('submit', async (e) => {
  e.preventDefault()
  const url = urlInput.value.trim()
  if (!url) return

  sendBtn.disabled = true
  setStatus('Sending to daemon…', false)

  try {
    const settings = await getSettings()
    if (!settings.port || !settings.token) {
      setStatus('Daemon not configured — open Settings.', false)
      return
    }

    const res = await sendToDaemon(settings, '/api/downloads', {
      url,
      referrer: '',
      headers: { 'User-Agent': 'dm/0.1.0-dev' },
      cookies: [],
    })

    if (res.ok) {
      setStatus('Download added to dm.', true)
      urlInput.value = ''
    } else {
      setStatus(`Daemon rejected (${res.status}).`, false)
    }
  } catch {
    setStatus('Daemon unreachable. Is dm running?', false)
  } finally {
    sendBtn.disabled = false
  }
})

openOptions.addEventListener('click', () => {
  void chrome.runtime.openOptionsPage()
})
