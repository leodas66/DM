import { writable } from 'svelte/store';
import { connectWebSocket } from './api.js';

export const downloads = writable([]);
export const error = writable(null);
export let ws = null;

export function startWS() {
  if (ws) {
    try { ws.close(); } catch (e) {}
  }
  ws = connectWebSocket((event) => {
    switch (event.type) {
      case 'added':
        downloads.update((list) => {
          if (list.find((d) => d.id === event.download.id)) return list;
          return [...list, event.download];
        });
        break;
      case 'progress':
        downloads.update((list) => list.map((d) => (d.id === event.download.id ? event.download : d)));
        break;
      case 'status':
        downloads.update((list) => list.map((d) => (d.id === event.download.id ? event.download : d)));
        break;
      case 'removed':
        downloads.update((list) => list.filter((d) => d.id !== event.downloadId));
        break;
    }
  });
}

export function stopWS() {
  if (ws) {
    try { ws.close(); } catch (e) {}
    ws = null;
  }
}
