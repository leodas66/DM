import { getSettings, saveSettings, DEFAULT_SETTINGS } from './storage'
import { sendToHost } from './types'

const form = document.getElementById('form') as HTMLFormElement
const enabledInput = document.getElementById('enabled') as HTMLInputElement
const minSizeInput = document.getElementById('minSize') as HTMLInputElement
const allowHostsInput = document.getElementById('allowHosts') as HTMLInputElement
const denyHostsInput = document.getElementById('denyHosts') as HTMLInputElement
const clearBtn = document.getElementById('clear') as HTMLButtonElement
const testBtn = document.getElementById('test') as HTMLButtonElement
const status = document.getElementById('status') as HTMLElement

function setStatus(text: string, ok: boolean) {
  status.textContent = text
  status.className = `status ${ok ? 'ok' : 'err'}`
}

function hosts(input: HTMLInputElement): string[] {
  return input.value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

async function load() {
  const settings = await getSettings()
  enabledInput.checked = settings.enabled
  minSizeInput.value = settings.minSize ? String(settings.minSize) : ''
  allowHostsInput.value = settings.allowHosts.join(', ')
  denyHostsInput.value = settings.denyHosts.join(', ')
}

form.addEventListener('submit', async (e) => {
  e.preventDefault()
  await saveSettings({
    enabled: enabledInput.checked,
    minSize: Number(minSizeInput.value) || 0,
    allowHosts: hosts(allowHostsInput),
    denyHosts: hosts(denyHostsInput),
  })
  setStatus('Saved.', true)
})

testBtn.addEventListener('click', async () => {
  testBtn.disabled = true
  setStatus('Checking the DM daemon\u2026', true)
  const response = await sendToHost({ command: 'ping' })
  testBtn.disabled = false
  if (response.ok) {
    setStatus(`Connected to the DM daemon (host ${response.version || 'unknown'}).`, true)
    return
  }
  setStatus(response.error || 'The DM daemon is not reachable.', false)
})

clearBtn.addEventListener('click', async () => {
  enabledInput.checked = DEFAULT_SETTINGS.enabled
  minSizeInput.value = ''
  allowHostsInput.value = ''
  denyHostsInput.value = ''
  await saveSettings({ ...DEFAULT_SETTINGS })
  setStatus('Cleared.', true)
})

void load()
