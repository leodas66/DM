import { writable } from 'svelte/store';
import { listDownloads } from './api.js';

export const downloads = writable([]);
export const error = writable(null);

export async function loadDownloads() {
  try {
    const list = await listDownloads();
    downloads.set(list);
  } catch (err) {
    error.set(err.message);
  }
}

export async function startWS() {
  await loadDownloads();
}

export async function stopWS() {
  // No-op for Wails bindings.
}
