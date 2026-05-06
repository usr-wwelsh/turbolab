<script>
  import { onMount } from 'svelte'
  import { getConfig, saveConfig } from './api.js'

  let cfg = { model: '', bits: 4, port: 7860, cpu_only: false, max_tokens: 2048, ctx_size: 2048 }
  let saved = false
  let error = null

  onMount(async () => {
    try { cfg = await getConfig() } catch (e) { error = e.message }
  })

  async function save() {
    error = null
    saved = false
    try {
      await saveConfig(cfg)
      saved = true
      setTimeout(() => saved = false, 2000)
    } catch (e) {
      error = e.message
    }
  }
</script>

<div class="pane">
  <div class="section-label">Settings</div>
  <p class="hint">Changes take effect on next server restart.</p>

  <div class="field">
    <label for="model">Default model</label>
    <input id="model" bind:value={cfg.model} placeholder="org/model-id" />
  </div>

  <div class="field">
    <label for="bits">KV cache bits</label>
    <select id="bits" bind:value={cfg.bits}>
      <option value={2}>2-bit</option>
      <option value={4}>4-bit</option>
      <option value={8}>8-bit</option>
    </select>
  </div>

  <div class="field">
    <label for="port">Port</label>
    <input id="port" type="number" bind:value={cfg.port} min="1024" max="65535" />
  </div>

  <div class="field">
    <label for="max_tokens">Max tokens (response length)</label>
    <input id="max_tokens" type="number" bind:value={cfg.max_tokens} min="128" max="131072" step="128" />
  </div>

  <div class="field">
    <label for="ctx_size">Context size (tokens)</label>
    <input id="ctx_size" type="number" bind:value={cfg.ctx_size} min="512" max="131072" step="512" />
  </div>

  <div class="field row">
    <label for="cpu_only">CPU only</label>
    <input id="cpu_only" type="checkbox" bind:checked={cfg.cpu_only} />
  </div>

  <button on:click={save}>Save</button>

  {#if saved}<div class="ok">Saved.</div>{/if}
  {#if error}<div class="err">{error}</div>{/if}
</div>

<style>
  .pane { padding: 1rem; display: flex; flex-direction: column; gap: 0.75rem; max-width: 480px; }
  .section-label { color: #555; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .hint { color: #555; font-size: 0.8rem; margin: 0; }
  .field { display: flex; flex-direction: column; gap: 0.25rem; }
  .field.row { flex-direction: row; align-items: center; gap: 0.5rem; }
  label { font-size: 0.8rem; color: #888; }
  input, select {
    padding: 0.4rem 0.6rem; background: #1a1a1a;
    border: 1px solid #333; color: #eee;
    border-radius: 4px; font-family: monospace; font-size: 0.9rem;
  }
  input[type="checkbox"] { width: 1rem; height: 1rem; padding: 0; }
  button {
    padding: 0.5rem 1.25rem; background: #7cf; color: #000;
    border: none; border-radius: 4px; cursor: pointer;
    font-weight: bold; align-self: flex-start;
  }
  .ok { color: #4f4; font-size: 0.85rem; }
  .err { color: #f66; font-size: 0.85rem; }
</style>
