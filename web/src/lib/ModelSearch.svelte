<script>
  import { onMount } from 'svelte'
  import { searchModels, loadModel, localModels, deleteModel } from './api.js'

  export let onLoad = () => {}

  let query = ''
  let results = []
  let local = []
  let searching = false
  let loadingModel = null
  let deletingModel = null
  let bits = 4

  onMount(async () => {
    local = await localModels()
  })

  async function search() {
    if (!query.trim()) return
    searching = true
    results = await searchModels(query)
    searching = false
  }

  async function load(modelId) {
    loadingModel = modelId
    loadNote = null
    const result = await loadModel(modelId, bits)
    loadingModel = null
    if (result.note) loadNote = result.note
    onLoad(modelId)
  }

  async function deleteModelClick(modelId) {
    if (!confirm(`Delete ${modelId}?`)) return
    deletingModel = modelId
    try {
      await deleteModel(modelId)
      local = local.filter(m => m.id !== modelId)
    } catch (e) {
      alert(`Failed to delete: ${e.message}`)
    } finally {
      deletingModel = null
    }
  }

  function onKey(e) {
    if (e.key === 'Enter') search()
  }

  const ggufTags = new Set(['gguf', 'ggml'])
  const incompatibleTags = new Set(['rwkv','mamba','ssm','granitemoehybrid','hybrid','retnet','hgrn'])

  let loadNote = null

  function isGGUFRepo(id) {
    return id && id.includes('/') && id.toUpperCase().endsWith('-GGUF')
  }

  function sizeLabel(m) {
    for (const tag of (m.tags ?? [])) {
      const t = tag.toLowerCase()
      if (t.endsWith('b') && /^\d/.test(t.slice(0, -1))) return tag.toUpperCase()
    }
    return '?'
  }

  function formatBytes(bytes) {
    if (!bytes) return '-'
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB'
    return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
  }

  function compatible(m) {
    // local models have pre-computed compat from config.json
    if (m.compatible != null) {
      if (!m.compatible && m.compat_reason === 'gguf') return { ok: 'gguf', label: 'llama-server' }
      return { ok: m.compatible ? 'yes' : 'no', label: m.compat_reason }
    }
    // GGUF repos (model ID ends with -GGUF) are loadable via llama-server
    if (isGGUFRepo(m.id)) return { ok: 'gguf', label: 'llama-server' }
    // search results: use tag heuristics
    const tags = new Set((m.tags ?? []).map(t => t.toLowerCase()))
    for (const tag of ggufTags) if (tags.has(tag)) return { ok: 'gguf', label: 'llama-server' }
    for (const bad of incompatibleTags) if (tags.has(bad)) return { ok: 'no', label: bad }
    if (m.library_name && m.library_name !== 'transformers') return { ok: 'no', label: m.library_name }
    if (tags.has('safetensors')) return { ok: 'yes', label: '✓' }
    return { ok: 'no', label: '?' }
  }
</script>

<div class="pane">
  <!-- Local models -->
  {#if local.length > 0}
    <div class="section-label">Downloaded</div>
    <div class="local-list">
      {#each local as m}
        {@const c = compatible(m)}
        <div class="local-row" class:incompatible={c.ok === 'no'} class:gguf={c.ok === 'gguf'}>
          <div class="local-info">
            <span class="model-id">{m.id}</span>
            <span class="model-size">{formatBytes(m.size_bytes)}</span>
            <span class="compat-badge" title={c.label}>
              {c.ok === 'yes' ? '✓' : c.ok === 'gguf' ? '⚡ ' + c.label : '✗ ' + c.label}
            </span>
          </div>
          <div class="local-actions">
            {#if c.ok === 'gguf' && isGGUFRepo(m.id)}
              <span class="gguf-badge">⚡ llama-server</span>
              <button class="load-btn" on:click={() => load(m.id)} disabled={loadingModel === m.id}>
                {loadingModel === m.id ? 'Loading...' : 'Load'}
              </button>
            {:else if c.ok === 'gguf'}
              <span class="gguf-hint" title="Load via CLI: turbolab serve --model /path/to/file.gguf">CLI only</span>
            {:else}
            <select bind:value={bits} class="bits-select-sm">
              <option value={2}>2-bit</option>
              <option value={4}>4-bit</option>
              <option value={8}>8-bit</option>
            </select>
            <button class="load-btn" on:click={() => load(m.id)} disabled={loadingModel === m.id}>
              {loadingModel === m.id ? 'Loading...' : 'Load'}
            </button>
            {/if}
            <button class="delete-btn" on:click={() => deleteModelClick(m.id)} disabled={deletingModel === m.id} title="Delete this model">
              {deletingModel === m.id ? 'Deleting...' : '×'}
            </button>
          </div>
        </div>
      {/each}
    </div>
  {:else}
    <div class="empty-local">No models downloaded yet.</div>
  {/if}

  {#if loadNote}
    <div class="load-note">{loadNote}</div>
  {/if}

  <!-- Search -->
  <div class="section-label">Search HuggingFace</div>
  <div class="search-row">
    <input
      bind:value={query}
      on:keydown={onKey}
      placeholder="e.g. qwen 1.5b instruct"
      class="search-input"
    />
    <select bind:value={bits} class="bits-select">
      <option value={2}>2-bit</option>
      <option value={4}>4-bit</option>
      <option value={8}>8-bit</option>
    </select>
    <button on:click={search} disabled={searching}>
      {searching ? '...' : 'Search'}
    </button>
  </div>

  {#if results.length > 0}
    <div class="results-wrap">
      <table class="results">
        <thead>
          <tr>
            <th>Model</th>
            <th>Size</th>
            <th>Downloads</th>
            <th>Compat</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each results as m}
            {@const c = compatible(m)}
            <tr class:incompatible={c.ok === 'no'} class:gguf={c.ok === 'gguf'}>
              <td class="model-id">{m.id}</td>
              <td class="size">{sizeLabel(m)}</td>
              <td>{m.downloads?.toLocaleString()}</td>
              <td class="compat" title={c.label}>{c.ok === 'yes' ? '✓' : c.ok === 'gguf' ? '⚡' : '✗'}</td>
              <td>
                {#if c.ok === 'gguf' && isGGUFRepo(m.id)}
                  <button
                    class="load-btn"
                    on:click={() => load(m.id)}
                    disabled={loadingModel === m.id}
                    title="Auto-downloads best quant via llama-server"
                  >
                    {loadingModel === m.id ? 'Downloading...' : '⚡ Load'}
                  </button>
                {:else if c.ok === 'gguf'}
                  <span class="gguf-hint" title="Load a local .gguf file via CLI: turbolab serve --model /path/to/file.gguf">CLI only</span>
                {:else}
                  <button
                    class="load-btn"
                    on:click={() => load(m.id)}
                    disabled={loadingModel === m.id}
                  >
                    {loadingModel === m.id ? 'Loading...' : 'Load'}
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .pane { padding: 1rem; display: flex; flex-direction: column; height: 100%; overflow-y: auto; gap: 0.5rem; }
  .section-label { color: #555; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; padding: 0.25rem 0; }
  .local-list { display: flex; flex-direction: column; gap: 0.25rem; margin-bottom: 0.5rem; }
  .local-row {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.4rem 0.6rem; background: #1a1a1a; border-radius: 4px;
  }
  .local-row.incompatible { opacity: 0.5; }
  .local-info { display: flex; align-items: center; gap: 0.5rem; }
  .model-size { font-size: 0.75rem; color: #666; min-width: 3.5rem; text-align: right; }
  .compat-badge { font-size: 0.75rem; color: #4f4; }
  .local-row.incompatible .compat-badge { color: #f66; }
  .local-row.gguf .compat-badge { color: #fa0; }
  tr.gguf .model-id { color: #7cf; }
  tr.gguf .compat { color: #fa0; }
  .load-note {
    padding: 0.4rem 0.75rem; background: #1a1200; border: 1px solid #fa0;
    border-radius: 4px; color: #fa0; font-size: 0.8rem;
  }
  .local-actions { display: flex; gap: 0.4rem; align-items: center; }
  .empty-local { color: #444; font-size: 0.85rem; margin-bottom: 0.5rem; }
  .search-row { display: flex; gap: 0.5rem; }
  .search-input {
    flex: 1; padding: 0.5rem 0.75rem;
    background: #1a1a1a; border: 1px solid #333; color: #eee;
    border-radius: 4px; font-family: monospace;
  }
  .bits-select, .bits-select-sm {
    padding: 0.5rem; background: #1a1a1a;
    border: 1px solid #333; color: #eee; border-radius: 4px;
  }
  .bits-select-sm { padding: 0.25rem; font-size: 0.8rem; }
  button {
    padding: 0.5rem 1rem; background: #7cf; color: #000;
    border: none; border-radius: 4px; cursor: pointer; font-weight: bold;
  }
  button:disabled { opacity: 0.5; cursor: default; }
  .results-wrap { overflow-y: visible; }
  .results { width: 100%; border-collapse: collapse; font-family: monospace; font-size: 0.85rem; }
  .results th { text-align: left; color: #555; padding: 0.4rem 0.5rem; border-bottom: 1px solid #222; }
  .results td { padding: 0.4rem 0.5rem; border-bottom: 1px solid #1a1a1a; color: #ccc; }
  .model-id { color: #7cf; }
  .size { color: #888; }
  .compat { text-align: center; }
  tr.incompatible .model-id { color: #555; }
  .load-btn { padding: 0.25rem 0.75rem; font-size: 0.8rem; }
  .delete-btn {
    padding: 0.25rem 0.5rem; font-size: 0.85rem; background: #f66; color: #fff;
    border: none; border-radius: 3px; cursor: pointer; font-weight: bold;
  }
  .delete-btn:hover:not(:disabled) { background: #d44; }
  .delete-btn:disabled { opacity: 0.5; cursor: default; }
  .gguf-hint { font-size: 0.75rem; color: #fa0; cursor: help; }
  .gguf-badge { font-size: 0.75rem; color: #fa0; }
</style>
