<script>
  import { onMount } from 'svelte';
  import DownloadList from './components/DownloadList.svelte';
  import AddDownload from './components/AddDownload.svelte';
  import Settings from './components/Settings.svelte';
  import { startWS, stopWS } from './store.js';

  let activeView = 'list';
  let addOpen = false;

  onMount(() => {
    startWS();
    return () => stopWS();
  });
</script>

<div class="shell">
  <header>
    <h1>dm</h1>
    <nav>
      <button class:active={activeView === 'list'} onclick={() => { activeView = 'list'; }}><span class="icon">📥</span> Downloads</button>
      <button class:active={activeView === 'settings'} onclick={() => { activeView = 'settings'; }}><span class="icon">⚙️</span> Settings</button>
    </nav>
  </header>

  {#if activeView === 'list'}
    <DownloadList />
    <button class="add-btn" onclick={() => addOpen = true}>+ Add download</button>
    <AddDownload {addOpen} on:close={() => addOpen = false} />
  {:else if activeView === 'settings'}
    <Settings />
  {/if}
</div>

<style>
  .shell { font-family: system-ui, -apple-system, sans-serif; max-width: 900px; margin: 0 auto; padding: 16px; }
  header { display: flex; align-items: center; gap: 16px; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid #e5e5e5; }
  h1 { margin: 0; font-size: 20px; letter-spacing: -0.5px; }
  nav { margin-left: auto; display: flex; gap: 12px; }
  nav button { background: transparent; border: 1px solid #ccc; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 14px; display: flex; align-items: center; gap: 6px; }
  nav button.active { background: #0066cc; color: #fff; border-color: #0066cc; }
  .add-btn { background: #0066cc; color: #fff; border: none; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; margin-top: 8px; }
  .add-btn:hover { background: #0052a3; }
</style>
