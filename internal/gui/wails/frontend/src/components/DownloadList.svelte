<script>
  import { onMount } from 'svelte';
  import DownloadRow from './DownloadRow.svelte';
  import { downloads, error, loadDownloads } from '../store.js';

  let list = [];

  $: if ($downloads) list = $downloads;

  onMount(() => {
    loadDownloads();
  });

  function formatBytes(bytes, decimals = 1) {
    if (!bytes) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
  }
</script>

<div class="list">
  {#if $error}
    <div class="error">{$error}</div>
  {/if}
  {#each list as d}
    <DownloadRow {d} />
  {:else}
    <div class="empty">No downloads yet. Add one to get started.</div>
  {/each}
</div>

<style>
  .list { display: flex; flex-direction: column; gap: 8px; }
  .error { color: #b00020; padding: 8px 12px; background: #fdecea; border: 1px solid #e0b3b3; border-radius: 6px; font-size: 13px; }
  .empty { color: #666; padding: 24px; text-align: center; }
</style>
