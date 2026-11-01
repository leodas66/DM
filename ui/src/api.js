const API_BASE = '';

const tokenKey = 'dmToken';

export function getToken() {
  return localStorage.getItem(tokenKey) || '';
}

export function setToken(token) {
  localStorage.setItem(tokenKey, token);
}

function headers() {
  const token = getToken();
  const h = { 'Content-Type': 'application/json' };
  if (token) h['Authorization'] = 'Bearer ' + token;
  return h;
}

async function request(path, opts = {}) {
  const res = await fetch(API_BASE + path, {
    ...opts,
    headers: { ...headers(), ...opts.headers },
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  if (res.status === 204) return null;
  return res.json();
}

export function listDownloads() {
  return request('/api/downloads');
}

export function addDownload(url, filename, dir, categoryId) {
  const body = { url };
  if (filename) body.filename = filename;
  if (dir) body.dir = dir;
  if (categoryId) body.categoryId = categoryId;
  return request('/api/downloads', { method: 'POST', body: JSON.stringify(body) });
}

export function pauseDownload(id) {
  return request('/api/downloads/' + encodeURIComponent(id) + '/pause', { method: 'POST' });
}

export function resumeDownload(id) {
  return request('/api/downloads/' + encodeURIComponent(id) + '/resume', { method: 'POST' });
}

export function cancelDownload(id) {
  return request('/api/downloads/' + encodeURIComponent(id) + '/cancel', { method: 'POST' });
}

export function removeDownload(id, deleteFile) {
  let path = '/api/downloads/' + encodeURIComponent(id);
  if (deleteFile) path += '?deleteFile=true';
  return request(path, { method: 'DELETE' });
}

export function getSettings() {
  return request('/api/settings');
}

export function updateSettings(settings) {
  return request('/api/settings', { method: 'PUT', body: JSON.stringify(settings) });
}

export function listCategories() {
  return request('/api/categories');
}

export function addCategory(name, defaultDir) {
  return request('/api/categories', {
    method: 'POST',
    body: JSON.stringify({ name, defaultDir }),
  });
}

export function deleteCategory(id) {
  return request('/api/categories/' + encodeURIComponent(id), { method: 'DELETE' });
}

export function connectWebSocket(onEvent) {
  const token = getToken();
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = protocol + '//' + window.location.host + '/api/ws?token=' + encodeURIComponent(token);
  const ws = new WebSocket(url);
  ws.onmessage = (ev) => {
    try {
      const event = JSON.parse(ev.data);
      onEvent(event);
    } catch (e) {
      // ignore malformed frames
    }
  };
  return ws;
}
