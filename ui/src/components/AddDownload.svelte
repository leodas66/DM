<script>
  import { createEventDispatcher } from 'svelte';
  import { addDownload, listDownloads, listCategories, addCategory } from '../api.js';
  import { downloads } from '../store.js';

  const dispatch = createEventDispatcher();

  export let open = false;

  let url = '';
  let filename = '';
  let dir = '';
  let categoryId = '';
  let categories = [];
  let newCategoryName = '';
  let newCategoryDir = '';
  let loading = false;
  let err = null;

  async function loadCategories() {
    try {
      categories = await listCategories();
    } catch (e) {
      // Non-fatal: category selection is optional.
      categories = [];
    }
  }

  async function addNewCategory() {
    const name = newCategoryName.trim();
    const defaultDir = newCategoryDir.trim();
    if (!name) return;
    try {
      const c = await addCategory(name, defaultDir || undefined);
      categories.push(c);
      categoryId = c.id;
      newCategoryName = '';
      newCategoryDir = '';
    } catch (e) {
      err = e.message;
    }
  }

  function close() {
    open = false;
    dispatch('close');
  }

  async function submit(e) {
    e.preventDefault();
    loading = true;
    err = null;
    try {
      await addDownload(url.trim(), filename.trim() || undefined, dir.trim() || undefined, categoryId || undefined);
      open = false;
      dispatch('close');
      const loaded = await listDownloads();
      downloads.set(loaded);
    } catch (e) {
      err = e.message;
    } finally {
      loading = false;
    }
  }

  $: if (open) loadCategories();
</script>

{#if open}
  <dialog class="dialog" onclose={close}>
    <form class="form" method="dialog" onsubmit={submit}>
      <h2>Add download</h2>
      <label>URL</label>
      <input required bind:value={url} placeholder="https://example.com/file.iso" />
      <label>Filename (optional)</label>
      <input bind:value={filename} placeholder="custom-name.iso" />
      <label>Directory (optional)</label>
      <input bind:value={dir} placeholder="uses category default or settings dir" />
      <label>Category</label>
      <select bind:value={categoryId}>
        <option value="">— none —</option>
        {#each categories as c}
          <option value={c.id}>{c.name} ({c.defaultDir})</option>
        {/each}
      </select>
      <div class="new-category">
        <label>New category</label>
        <div class="new-category-row">
          <input bind:value={newCategoryName} placeholder="Category name" />
          <input bind:value={newCategoryDir} placeholder="Default directory (optional)" />
          <button type="button" onclick={addNewCategory} disabled={loading}>+</button>
        </div>
      </div>
      {#if err}<p class="error">{err}</p>{/if}
      <div class="buttons">
        <button type="button" onclick={close}>Cancel</button>
        <button type="submit" disabled={loading}>{loading ? 'Adding…' : 'Add download'}</button>
      </div>
    </form>
  </dialog>
  <div class="backdrop" onclick={close}></div>
{/if}

<style>
  .dialog { border: none; border-radius: 10px; padding: 24px; max-width: 520px; box-shadow: 0 10px 30px rgba(0,0,0,0.2); position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); z-index: 100; background: #fff; }
  .backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.4); z-index: 99; }
  .form { display: flex; flex-direction: column; gap: 12px; }
  h2 { margin: 0 0 8px; font-size: 18px; }
  label { font-size: 12px; font-weight: 600; color: #333; }
  input, select { padding: 8px; font-size: 14px; border: 1px solid #ccc; border-radius: 6px; width: 100%; box-sizing: border-box; }
  .new-category { margin-top: 4px; }
  .new-category-row { display: flex; gap: 6px; margin-top: 4px; }
  .new-category-row input { flex: 1; }
  .new-category-row button { width: 34px; padding: 0; }
  .error { color: #b00020; font-size: 12px; margin: 0; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
  button { padding: 7px 16px; font-size: 14px; border-radius: 6px; border: 1px solid #ccc; background: #fff; cursor: pointer; }
  button[type="submit"] { background: #0066cc; color: #fff; border-color: #0066cc; }
  button[type="submit"]:hover { background: #0052a3; }
  button[type="submit"][disabled] { opacity: 0.6; cursor: not-allowed; }
</style>
