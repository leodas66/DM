<script>
  import { getSettings, updateSettings, getToken, setToken } from '../api.js';
  import { listCategories, addCategory, deleteCategory } from '../api.js';

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

  let categories = [];
  let newCatName = '';
  let newCatDir = '';
  let catLoading = false;
  let catErr = null;

  async function load() {
    try {
      const s = await getSettings();
      settings = { ...settings, ...s };
      token = getToken();
      categories = await listCategories();
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

  async function addNewCategory() {
    const name = newCatName.trim();
    const defaultDir = newCatDir.trim();
    if (!name) return;
    catLoading = true;
    catErr = null;
    try {
      const c = await addCategory(name, defaultDir || undefined);
      categories = [...categories, c];
      newCatName = '';
      newCatDir = '';
    } catch (e) {
      catErr = e.message;
    } finally {
      catLoading = false;
    }
  }

  async function removeCategory(id) {
    if (!confirm('Remove this category? Downloads using it will keep their category reference.')) return;
    try {
      await deleteCategory(id);
      categories = categories.filter((c) => c.id !== id);
    } catch (e) {
      catErr = e.message;
    }
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

  <section>
    <h3>Categories</h3>
    <p class="hint">Categories set a default download directory. Pick one when adding a download.</p>
    {#if catErr}<div class="error">{catErr}</div>{/if}
    <ul class="category-list">
      {#each categories as c}
        <li class="category-item">
          <span>{c.name} — {c.defaultDir || '(no default)'}</span>
          <button type="button" onclick={() => removeCategory(c.id)} class="remove-cat">Remove</button>
        </li>
      {:else}
        <li class="category-empty">No categories yet.</li>
      {/each}
    </ul>
    <div class="new-category">
      <label>New category</label>
      <div class="new-category-row">
        <input bind:value={newCatName} placeholder="Category name" />
        <input bind:value={newCatDir} placeholder="Default directory (optional)" />
        <button type="button" onclick={addNewCategory} disabled={catLoading}>+</button>
      </div>
    </div>
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

  .category-list { list-style: none; padding: 0; margin: 12px 0 8px; border: 1px solid #ddd; border-radius: 6px; background: #fff; }
  .category-item { display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; border-bottom: 1px solid #eee; font-size: 13px; }
  .category-item:last-child { border-bottom: none; }
  .category-item span { flex: 1; color: #333; }
  .category-item .remove-cat { padding: 4px 10px; font-size: 12px; color: #b00020; border: 1px solid #b00020; background: #fff; }
  .category-item .remove-cat:hover { background: #fdecea; }
  .category-empty { padding: 8px 12px; color: #666; font-size: 13px; }
  .new-category { margin-top: 8px; }
  .new-category-row { display: flex; gap: 6px; margin-top: 4px; }
  .new-category-row input { flex: 1; padding: 7px 10px; font-size: 13px; border: 1px solid #ccc; border-radius: 6px; box-sizing: border-box; }
  .new-category-row button { width: 34px; padding: 0; }
</style>
