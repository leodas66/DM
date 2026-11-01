import { getSettings, saveSettings, DEFAULT_SETTINGS } from './storage'

const form = document.getElementById('form') as HTMLFormElement
const portInput = document.getElementById('port') as HTMLInputElement
const tokenInput = document.getElementById('token') as HTMLInputElement
const minSizeInput = document.getElementById('minSize') as HTMLInputElement
const allowHostsInput = document.getElementById('allowHosts') as HTMLInputElement
const denyHostsInput = document.getElementById('denyHosts') as HTMLInputElement
const clearBtn = document.getElementById('clear') as HTMLButtonElement
const status = document.getElementById('status') as HTMLElement

function setStatus(text: string, ok: boolean) {
  status.textContent = text
  status.className = `status ${ok ? 'ok' : 'err'}`
}

async function load() {
  const settings = await getSettings()
  portInput.value = settings.port ? String(settings.port) : ''
  tokenInput.value = settings.token
  minSizeInput.value = settings.minSize ? String(settings.minSize) : ''
  allowHostsInput.value = settings.allowHosts.join(', ')
  denyHostsInput.value = settings.denyHosts.join(', ')
}

form.addEventListener('submit', async (e) => {
  e.preventDefault()
  const port = Number(portInput.value) || 0
  const minSize = Number(minSizeInput.value) || 0
  const allowHosts = allowHostsInput.value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  const denyHosts = denyHostsInput.value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

  await saveSettings({
    port,
    token: tokenInput.value.trim(),
    minSize,
    allowHosts,
    denyHosts,
  })
  setStatus('Saved.', true)
})

clearBtn.addEventListener('click', async () => {
  portInput.value = ''
  tokenInput.value = ''
  minSizeInput.value = ''
  allowHostsInput.value = ''
  denyHostsInput.value = ''
  await saveSettings({
    ...DEFAULT_SETTINGS,
    port: 0,
    token: '',
  })
  setStatus('Cleared.', true)
})

load()
