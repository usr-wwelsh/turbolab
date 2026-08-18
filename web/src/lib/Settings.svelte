<script>
  import { onMount } from 'svelte'
  import { getConfig, saveConfig } from './api.js'

  let cfg = { model: '', bits: 4, port: 7860, cpu_only: false, max_tokens: 2048, ctx_size: 2048, recycle_rss_mb: 0, system_prompt: '', cot_prompt_enabled: false, self_consistency_n: 0, self_consistency_show_all: false, temperature: null, top_p: null, top_k: null, min_p: null, repeat_penalty: null, seed: null }
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
    <label for="system_prompt">Default system prompt</label>
    <textarea id="system_prompt" bind:value={cfg.system_prompt} rows="3" placeholder="You are a helpful assistant."></textarea>
    <span class="hint">Used when a request doesn't include its own system message. A client-supplied one always wins.</span>
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

  <div class="field">
    <label for="recycle_rss_mb">Auto-recycle RSS limit (MB)</label>
    <input id="recycle_rss_mb" type="number" bind:value={cfg.recycle_rss_mb} min="0" step="256" />
    <span class="hint">Restart the inference server when its memory exceeds this. 0 disables.</span>
  </div>

  <div class="field row">
    <label for="cot_prompt_enabled">Chain-of-thought prompting</label>
    <input id="cot_prompt_enabled" type="checkbox" bind:checked={cfg.cot_prompt_enabled} />
  </div>

  <div class="field">
    <label for="self_consistency_n">Self-consistency samples</label>
    <input id="self_consistency_n" type="number" bind:value={cfg.self_consistency_n} min="0" step="1" />
    <span class="hint">Fire N parallel completions and vote on the most common answer. 0 or 1 disables.</span>
  </div>

  <div class="field">
    <label for="self_consistency_show_all">Show all self-consistency candidates</label>
    <input id="self_consistency_show_all" type="checkbox" bind:checked={cfg.self_consistency_show_all} />
    <span class="hint">Dev mode: attach every parallel sample and its vote count to the response, not just the winner.</span>
  </div>

  <div class="field">
    <label for="temperature">Temperature</label>
    <input id="temperature" type="number" bind:value={cfg.temperature} min="0" max="2" step="0.05" placeholder="backend default" />
    <span class="hint">Sampling randomness. 0 is greedy/deterministic. Leave blank to use the backend's default.</span>
  </div>

  <div class="field">
    <label for="top_p">Top-p</label>
    <input id="top_p" type="number" bind:value={cfg.top_p} min="0" max="1" step="0.05" placeholder="backend default" />
    <span class="hint">Nucleus sampling cutoff. Leave blank to use the backend's default.</span>
  </div>

  <div class="field">
    <label for="top_k">Top-k</label>
    <input id="top_k" type="number" bind:value={cfg.top_k} min="0" step="1" placeholder="backend default" />
    <span class="hint">Restrict sampling to the k most likely tokens. Leave blank to use the backend's default.</span>
  </div>

  <div class="field">
    <label for="min_p">Min-p</label>
    <input id="min_p" type="number" bind:value={cfg.min_p} min="0" max="1" step="0.01" placeholder="backend default" />
    <span class="hint">Keep tokens at least this fraction as likely as the top token. Leave blank to use the backend's default.</span>
  </div>

  <div class="field">
    <label for="repeat_penalty">Repeat penalty</label>
    <input id="repeat_penalty" type="number" bind:value={cfg.repeat_penalty} min="0" step="0.05" placeholder="backend default" />
    <span class="hint">Penalize tokens already used, to curb loops/repetition. 1.0 = no penalty.</span>
  </div>

  <div class="field">
    <label for="seed">Seed</label>
    <input id="seed" type="number" bind:value={cfg.seed} step="1" placeholder="random" />
    <span class="hint">Fixed RNG seed for reproducible output. Leave blank for random.</span>
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
  .pane {
    padding: 1rem; display: flex; flex-direction: column; gap: 0.75rem;
    max-width: 480px; height: 100%; overflow-y: auto;
  }
  .section-label { color: var(--fg-5); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .hint { color: var(--fg-5); font-size: 0.8rem; margin: 0; }
  .field { display: flex; flex-direction: column; gap: 0.25rem; }
  .field.row { flex-direction: row; align-items: center; gap: 0.5rem; }
  label { font-size: 0.8rem; color: var(--fg-3); }
  input, select, textarea {
    padding: 0.4rem 0.6rem; background: var(--bg-card);
    border: 1px solid var(--border); color: var(--fg);
    border-radius: 4px; font-family: monospace; font-size: 0.9rem;
  }
  textarea { resize: vertical; }
  input[type="checkbox"] { width: 1rem; height: 1rem; padding: 0; }
  button {
    padding: 0.5rem 1.25rem; background: var(--accent); color: var(--accent-fg);
    border: none; border-radius: 4px; cursor: pointer;
    font-weight: bold; align-self: flex-start;
  }
  .ok { color: var(--success); font-size: 0.85rem; }
  .err { color: var(--error); font-size: 0.85rem; }
</style>
