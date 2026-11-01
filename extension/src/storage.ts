const STORAGE_KEY = 'dmSettings'

export type DaemonSettings = import('./types').DaemonSettings

export const DEFAULT_SETTINGS: DaemonSettings = {
  port: 0,
  token: '',
  minSize: 0,
  allowHosts: [],
  denyHosts: [],
}

export async function getSettings(): Promise<DaemonSettings> {
  const res = await chrome.storage.local.get(STORAGE_KEY)
  const saved = res[STORAGE_KEY] as Partial<DaemonSettings> | undefined
  return { ...DEFAULT_SETTINGS, ...saved }
}

export async function saveSettings(partial: Partial<DaemonSettings>): Promise<void> {
  const current = await getSettings()
  const merged = { ...current, ...partial }
  await chrome.storage.local.set({ [STORAGE_KEY]: merged })
}

export async function clearSettings(): Promise<void> {
  await chrome.storage.local.remove(STORAGE_KEY)
}
