const STORAGE_KEY = 'dmSettings'

export type CaptureSettings = import('./types').CaptureSettings

export const DEFAULT_SETTINGS: CaptureSettings = {
  enabled: true,
  minSize: 0,
  allowHosts: [],
  denyHosts: [],
}

export async function getSettings(): Promise<CaptureSettings> {
  const api = (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome
  const res = await api.storage.local.get(STORAGE_KEY)
  const saved = res[STORAGE_KEY] as Partial<CaptureSettings> | undefined
  return { ...DEFAULT_SETTINGS, ...saved }
}

export async function saveSettings(partial: Partial<CaptureSettings>): Promise<void> {
  const api = (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome
  const current = await getSettings()
  const merged = { ...current, ...partial }
  await api.storage.local.set({ [STORAGE_KEY]: merged })
}

export async function clearSettings(): Promise<void> {
  const api = (globalThis as unknown as { browser?: typeof chrome }).browser ?? chrome
  await api.storage.local.remove(STORAGE_KEY)
}
