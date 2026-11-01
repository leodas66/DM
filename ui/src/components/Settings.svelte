<script>
  import { getSettings, updateSettings, getToken, setToken } from '../api.js';

  let settings = {
    dir: '',
    maxConcurrent: 3,
    connections: 8,
    minSegmentSize: 1048576,
    globalLimit: 0,
    perDownloadLimit: 0,
    saveInterval: '1s',
  };
  let token = '';
  let loading = false;
  let saving = false;
  let err = null;
  let msg = null;

  async function load() {
    try {
      const s = await getSettings();
      settings = { ...settings, ...s };
      token = getToken();
    } catch (e) {
      err = 'Cannot reach daemon: ' + e.message;
    }
  }

  async function save() {
    saving = true;
    err = null;
    msg = null;
    try {
      const body = {
        dir: settings.dir || undefined,
        maxConcurrent: settings.maxConcurrent || undefined,
        connections: settings.connections || undefined,
        minSegmentSize: settings.minSegmentSize || undefined,
        globalLimit: settings.globalLimit || undefined,
        perDownloadLimit: settings.perDownloadLimit || undefined,
        saveInterval: settings.saveInterval || undefined,
      };
      await updateSettings(body);
      msg = 'Settings saved.';
      setTimeout(() => (msg = null), 3000);
    } catch (e) {
      err = e.message;
    } finally {
      saving = false;
    }
  }

  function saveToken() {
    if (token.trim()) setToken(token.trim());
    msg = 'Token saved.';
    setTimeout(() => (msg = null), 3000);
  }

  function formatBytes(bytes, decimals = 0) {
    if (!bytes) return '0';
    const k = 1024;
    const sizes = ['B', 'KiB', 'MiB', 'GiB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
  }

  load();
</script>

<div class="settings">
  <h2>Settings</h2>

  {#if err}<div class="error">{err}</div>{/if}
  {#if msg}<div class="msg">{msg}</div>{/if}

  <section>
    <h3>Daemon access</h3>
    <p class="hint">Get the bearer token from <code>~/.config/dm/token</code> on the machine running dm.</p>
    <label>Bearer token</label>
    <input type="password" bind:value={token} placeholder="paste token here" />
    <button onclick={saveToken} disabled={!token.trim()}>Save token</button>
  </section>

  <section>
    <h3>Download behavior</h3>
    <label>Default download directory</label>
    <input bind:value={settings.dir} placeholder="/home/user/downloads" />
    <label>Max concurrent downloads</label>
    <input type="number" min="1" bind:value={settings.maxConcurrent} />
    <label>Connections per download (segments)</label>
    <input type="number" min="1" bind:value={settings.connections} />
    <label>Minimum segment size (bytes)</label>
    <input type="number" min="0" bind:value={settings.minSegmentSize} />
  </section>

  <section>
    <h3>Speed limits</h3>
    <label>Global limit (bytes/s, 0 = unlimited)</label>
    <input type="number" min="0" bind:value={settings.globalLimit} />
    <label>Per-download limit (bytes/s, 0 = unlimited)</label>
    <input type="number" min="0" bind:value={settings.perDownloadLimit} />
    <label>Save interval</label>
    <input bind:value={settings.saveInterval} placeholder="1s" />
  </section>

  <div class="actions">
    <button onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save settings'}</button>
  </div>
</div>

<style>
  .settings { max-width: 560px; }
  h2 { margin: 0 0 16px; font-size: 20px; }
  section { margin-bottom: 24px; padding: 16px; border: 1px solid #eee; border-radius: 8px; background: #fafafa; }
  h3 { margin: 0 0 12px; font-size: 15px; color: #111; }
  .hint { font-size: 12px; color: #666; margin-bottom: 10px; }
  label { display: block; font-size: 12px; font-weight: 600; color: #333; margin-bottom: 4px; }
  input { width: 100%; padding: 7px 10px; font-size: 13px; border: 1px solid #ccc; border-radius: 6px; box-sizing: border-box; margin-bottom: 10px; }
  .error { color: #b00020; padding: 8px 12px; background: #fdecea; border: 1px solid #e0b3b3; border-radius: 6px; font-size: 13px; margin-bottom: 12px; }
  .msg { color: #1a7a1a; padding: 8px 12px; background: #eef7ee; border: 1px solid #b9dbb9; border-radius: 6px; font-size: 13px; margin-bottom: 12px; }
  .actions { display: flex; gap: 8px; }
  button { padding: 8px 16px; font-size: 14px; border-radius: 6px; border: 1px solid #0066cc; background: #0066cc; color: #fff; cursor: pointer; }
  button:hover { background: #0052a3; }
  button:disabled { opacity: 0.6; cursor: not-allowed; }
</style>
