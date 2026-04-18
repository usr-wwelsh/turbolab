<script>
  export let status = null
  export let crashed = false
  export let inferring = false
  export let onShowLogs = () => {}

</script>

<div class="status-bar">
  <span class="logo">⚡ turbolab</span>
  {#if status}
    <span class="model" class:running={status.running && !status.loading} class:loading={status.loading}>
      {status.loading ? `loading ${status.model}…` : status.running ? status.model : 'no model loaded'}
    </span>
    <div class="meters">
      <div class="meter" title="CPU usage">
        <span class="meter-label">CPU</span>
        <div class="bar">
          <div class="fill cpu" style="width: {Math.min(100, status.cpu_percent ?? 0).toFixed(0)}%"></div>
        </div>
        <span class="meter-val">{(status.cpu_percent ?? 0).toFixed(0)}%</span>
      </div>
      <div class="meter" title="RAM">
        <span class="meter-label">RAM</span>
        <div class="bar">
          <div class="fill ram" style="width: {Math.min(100, 100 - (status.ram_available_gb / 16) * 100)}%"></div>
        </div>
        <span class="meter-val">{status.ram_available_gb?.toFixed(1)} GB free</span>
      </div>
      <div class="meter" title="Disk usage">
        <span class="meter-label">Disk</span>
        <span class="meter-val">{status.disk_available_gb?.toFixed(1)} GB / {status.disk_used_gb?.toFixed(1)} GB</span>
      </div>
    </div>
  {/if}
  {#if inferring}
    <span class="inferring">⬡ inferring</span>
  {/if}
  {#if crashed}
    <button class="crash-badge" on:click={onShowLogs}>⚠ crashed — view logs</button>
  {/if}
</div>

<style>
  .status-bar {
    display: flex; align-items: center; gap: 1.5rem;
    padding: 0.6rem 1.2rem; background: #111;
    border-bottom: 1px solid #222; font-size: 0.85rem; font-family: monospace;
    flex-shrink: 0;
  }
  .logo { color: #7cf; font-weight: bold; }
  .model { color: #666; }
  .model.running { color: #4f4; }
  .model.loading { color: #fa0; animation: pulse 1.5s ease-in-out infinite; }
  .meters { display: flex; gap: 1rem; margin-left: auto; align-items: center; }
  .meter { display: flex; align-items: center; gap: 0.4rem; }
  .meter-label { color: #555; font-size: 0.75rem; width: 2.5rem; }
  .meter-val { color: #aaa; font-size: 0.75rem; white-space: nowrap; margin-left: 0.5rem; }
  .bar { width: 60px; height: 5px; background: #222; border-radius: 3px; overflow: hidden; }
  .fill { height: 100%; border-radius: 3px; transition: width 1s; }
  .fill.cpu { background: #a7f; }
  .fill.ram { background: #7cf; }
  .inferring {
    color: #a7f; font-size: 0.8rem; animation: pulse 1.5s ease-in-out infinite;
  }
  .crash-badge {
    background: none; border: 1px solid #f66; color: #f66;
    padding: 0.2rem 0.6rem; border-radius: 4px; cursor: pointer;
    font-family: monospace; font-size: 0.8rem; animation: pulse 2s infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
