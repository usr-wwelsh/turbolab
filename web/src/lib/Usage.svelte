<script>
  import { onMount } from 'svelte'
  import { getUsage } from './api.js'

  const INPUT_RATE  = 3.0   // $ per 1M input tokens
  const OUTPUT_RATE = 15.0  // $ per 1M output tokens

  let days = 30
  let data = null
  let loading = false

  async function load() {
    loading = true
    try { data = await getUsage(days) } catch {}
    loading = false
  }

  onMount(load)

  function fmt(n) {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M'
    if (n >= 1_000)     return (n / 1_000).toFixed(1) + 'K'
    return String(n)
  }

  function savings(prompt, completion) {
    return (prompt / 1_000_000 * INPUT_RATE + completion / 1_000_000 * OUTPUT_RATE)
  }

  // SVG chart constants
  const W  = 600, H  = 180
  const PL = 52,  PR = 12, PT = 10, PB = 32
  const cW = W - PL - PR
  const cH = H - PT - PB

  $: maxVal = (data?.days?.length)
    ? Math.max(1, ...data.days.map(d => d.prompt_tokens + d.completion_tokens))
    : 1

  $: bw  = data?.days?.length ? Math.max(2, cW / data.days.length * 0.7) : 8
  $: gap = data?.days?.length ? cW / data.days.length : 20

  function barX(i)  { return PL + i * gap + (gap - bw) / 2 }
  function yLabel(v) { return fmt(v) }

  $: yTicks = [0, 0.25, 0.5, 0.75, 1].map(f => ({
    v: maxVal * f,
    y: PT + cH - f * cH,
  }))

  $: labelStep = data?.days?.length > 14 ? Math.ceil(data.days.length / 7) : 1
</script>

<div class="pane">
  <div class="header">
    <span class="section-label">Usage</span>
    <div class="day-btns">
      {#each [7, 30, 90] as d}
        <button class:active={days === d} on:click={() => { days = d; load() }}>{d}d</button>
      {/each}
    </div>
  </div>

  {#if loading}
    <div class="hint">Loading...</div>
  {:else if !data || data.total_requests === 0}
    <div class="hint">No usage data yet — start chatting to track tokens.</div>
  {:else}
    <div class="chart-wrap">
      <svg viewBox="0 0 {W} {H}" width="100%" preserveAspectRatio="none">
        <!-- grid lines + y labels -->
        {#each yTicks as t}
          <line x1={PL} y1={t.y} x2={W - PR} y2={t.y} stroke="#1c1c1c" stroke-width="1"/>
          <text x={PL - 4} y={t.y + 4} text-anchor="end" fill="#444" font-size="11" font-family="monospace">
            {yLabel(t.v)}
          </text>
        {/each}

        <!-- stacked bars -->
        {#each data.days as day, i}
          {@const ph = (day.prompt_tokens     / maxVal) * cH}
          {@const ch = (day.completion_tokens / maxVal) * cH}
          {@const th = ph + ch}
          <!-- input (bottom, blue) -->
          <rect x={barX(i)} y={PT + cH - th} width={bw} height={ph} fill="#2a5fc4bb"/>
          <!-- output (top, cyan) -->
          <rect x={barX(i)} y={PT + cH - ch} width={bw} height={ch} fill="#55ccff99"/>
        {/each}

        <!-- x labels -->
        {#each data.days as day, i}
          {#if i % labelStep === 0}
            <text x={barX(i) + bw / 2} y={H - 6}
              text-anchor="middle" fill="#444" font-size="10" font-family="monospace">
              {day.date.slice(5)}
            </text>
          {/if}
        {/each}
      </svg>

      <div class="legend">
        <span class="dot input"></span>input
        <span class="dot output"></span>output
      </div>
    </div>

    <div class="stats">
      <div class="stat">
        <div class="stat-val">{data.total_requests}</div>
        <div class="stat-lbl">requests</div>
      </div>
      <div class="stat">
        <div class="stat-val">{fmt(data.total_prompt_tokens)}</div>
        <div class="stat-lbl">input tokens</div>
      </div>
      <div class="stat">
        <div class="stat-val">{fmt(data.total_completion_tokens)}</div>
        <div class="stat-lbl">output tokens</div>
      </div>
      <div class="stat accent">
        <div class="stat-val">${savings(data.total_prompt_tokens, data.total_completion_tokens).toFixed(4)}</div>
        <div class="stat-lbl">saved vs API</div>
      </div>
    </div>

    <div class="rates-note">
      Rates: $3.00 / 1M input · $15.00 / 1M output
    </div>
  {/if}
</div>

<style>
  .pane { padding: 1rem; display: flex; flex-direction: column; gap: 1rem; max-width: 680px; }
  .header { display: flex; align-items: center; justify-content: space-between; }
  .section-label { color: #555; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .hint { color: #555; font-size: 0.85rem; }

  .day-btns { display: flex; gap: 0.25rem; }
  .day-btns button {
    padding: 0.2rem 0.6rem; background: none; border: 1px solid #333;
    color: #555; cursor: pointer; font-family: monospace; font-size: 0.8rem; border-radius: 3px;
  }
  .day-btns button.active { color: #7cf; border-color: #7cf; }

  .chart-wrap {
    background: #0e0e0e; border: 1px solid #1e1e1e; border-radius: 4px; padding: 0.5rem 0.5rem 0.25rem;
  }
  .legend {
    display: flex; gap: 1rem; font-size: 0.75rem; color: #555;
    justify-content: flex-end; padding-top: 0.25rem;
  }
  .dot { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 4px; vertical-align: middle; }
  .dot.input  { background: #2a5fc4bb; }
  .dot.output { background: #55ccff99; }

  .stats { display: flex; gap: 0.75rem; flex-wrap: wrap; }
  .stat {
    background: #111; border: 1px solid #1e1e1e; border-radius: 4px;
    padding: 0.6rem 1rem; flex: 1; min-width: 100px;
  }
  .stat.accent { border-color: #2a2a14; background: #141408; }
  .stat-val { font-size: 1.2rem; color: #eee; }
  .stat.accent .stat-val { color: #cf4; }
  .stat-lbl { font-size: 0.72rem; color: #555; margin-top: 0.15rem; }

  .rates-note { font-size: 0.72rem; color: #383838; }
</style>
