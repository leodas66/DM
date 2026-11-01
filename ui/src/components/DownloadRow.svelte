<script>
  import { pauseDownload, resumeDownload, cancelDownload, removeDownload } from '../api.js';
  import { downloads, error } from '../store.js';

  export let d;

  let statusError = null;

  function pct() {
    if (!d.size || d.size <= 0) return 0;
    const total = d.segments?.reduce((sum, s) => sum + s.downloaded, 0) ?? 0;
    return Math.min(100, Math.round((total / d.size) * 100));
  }

  async function action(fn, label) {
    statusError = null;
    try {
      const updated = await fn(d.id);
      downloads.update((list) => list.map((x) => (x.id === d.id ? updated : x)));
    } catch (err) {
      statusError = err.message;
      error.set(err.message);
      setTimeout(() => error.set(null), 4000);
    }
  }

  function onRemove() {
    if (!confirm('Remove this download?')) return;
    action(() => removeDownload(d.id, false), 'remove');
  }
</script>

<div class="row">
  <div class="info">
    <div class="name">{d.filename || d.url}</div>
    <div class="meta">
      <span class="badge status-{d.status}">{d.status}</span>
      {#if d.status === 'failed' && d.error}<span class="err">{d.error}</span>{/if}
      {#if statusError}<span class="err">{statusError}</span>{/if}
    </div>
  </div>

  <div class="progress">
    <div class="track"><div class="fill" style:width={pct() + '%'}></div></div>
    <div class="label">{pct()}%</div>
  </div>

  <div class="actions">
    {#if d.status === 'queued' || d.status === 'probing' || d.status === 'running'}
      <button onclick={() => action(pauseDownload, 'pause')} disabled={statusError}>Pause</button>
    {:else if d.status === 'paused'}
      <button onclick={() => action(resumeDownload, 'resume')} disabled={statusError}>Resume</button>
    {/if}
    <button onclick={() => action(cancelDownload, 'cancel')} disabled={statusError}>Cancel</button>
    <button onclick={onRemove} class="remove">Remove</button>
  </div>
</div>

<style>
  .row { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border: 1px solid #ddd; border-radius: 8px; background: #fff; }
  .info { flex: 1; min-width: 0; }
  .name { font-size: 14px; font-weight: 600; color: #111; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .meta { display: flex; align-items: center; gap: 8px; margin-top: 4px; font-size: 12px; color: #555; }
  .badge { display: inline-block; padding: 1px 6px; border-radius: 10px; font-size: 11px; font-weight: 600; text-transform: uppercase; }
  .badge.status-queued { background: #eef4ff; color: #0066cc; }
  .badge.status-probing { background: #fff4e6; color: #b26a00; }
  .badge.status-running { background: #e6f4ea; color: #1a7a1a; }
  .badge.status-paused { background: #f4e6e6; color: #a33; }
  .badge.status-completed { background: #e6f4ea; color: #1a7a1a; }
  .badge.status-failed, .badge.status-canceled { background: #fdecea; color: #b00020; }
  .err { color: #b00020; font-size: 11px; }
  .progress { width: 200px; flex-shrink: 0; }
  .track { height: 8px; background: #eee; border-radius: 4px; overflow: hidden; margin-bottom: 4px; }
  .fill { height: 100%; background: #0066cc; border-radius: 4px; width: 0%; transition: width 0.2s ease; }
  .label { font-size: 11px; color: #666; text-align: center; }
  .actions { display: flex; gap: 6px; flex-shrink: 0; }
  .actions button { padding: 4px 10px; font-size: 12px; border-radius: 6px; border: 1px solid #ccc; background: #fff; cursor: pointer; }
  .actions button:hover { background: #f5f5f5; }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .actions button.remove { color: #b00020; border-color: #b00020; }
  .actions button.remove:hover { background: #fdecea; }
</style>
