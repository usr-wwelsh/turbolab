<script>
  export let lines = []
  export let onClose = () => {}
</script>

<div class="panel">
  <div class="header">
    <span>Logs</span>
    <button on:click={onClose}>✕</button>
  </div>
  <div class="body">
    {#each lines as line}
      <div class="line" class:crash={line.includes('crashed')} class:err={line.includes('Error') || line.includes('error')}>
        {line}
      </div>
    {/each}
    {#if lines.length === 0}
      <div class="empty">No logs yet.</div>
    {/if}
  </div>
</div>

<style>
  .panel {
    position: fixed; bottom: 0; left: 0; right: 0;
    height: 240px; background: var(--bg-deep); border-top: 1px solid var(--border);
    display: flex; flex-direction: column; z-index: 100; font-family: monospace;
  }
  .header {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.3rem 0.75rem; background: var(--bg-elev); border-bottom: 1px solid var(--border-subtle);
    font-size: 0.8rem; color: var(--fg-5);
  }
  .header button {
    background: none; border: none; color: var(--fg-5); cursor: pointer;
    font-size: 0.9rem; padding: 0; font-family: monospace;
  }
  .header button:hover { color: var(--fg); }
  .body { flex: 1; overflow-y: auto; padding: 0.5rem 0.75rem; }
  .line { font-size: 0.75rem; color: var(--fg-3); white-space: pre-wrap; line-height: 1.5; }
  .line.crash { color: var(--error); }
  .line.err { color: var(--warn); }
  .empty { color: var(--fg-6); font-size: 0.8rem; }
</style>
