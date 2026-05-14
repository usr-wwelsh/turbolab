<script>
  import { onMount, onDestroy } from 'svelte'
  import {
    listMemories, searchMemories, semanticSearchMemories, rebuildEmbeddings,
    addMemory, deleteMemory, relateMemories, unrelateMemories, getMemoryGraph,
    convertFile, getConfig, saveConfig, memoryStats
  } from './api.js'


  let tab = 'memories'
  let memories = []
  let selected = null
  let searchQ = ''
  let searching = false
  let semanticMode = false
  let searchScores = {}
  let error = null

  // Add tab
  let addContent = ''
  let addSource = ''
  let addTags = ''
  let addError = null
  let addOk = false
  let converting = false
  let convertPreview = ''
  let convertFilename = ''
  let draggingOver = false

  // Relate
  let relFromID = ''
  let relToID = ''
  let relType = 'related'
  let relError = null
  let relOk = false

  // Graph
  let canvas
  let simNodes = []
  let graphEdges = []
  let graphSelected = null
  let animFrame
  let draggingNode = null

  // MCP + inject
  let mcpEnabled = false
  let memInject = false
  $: mcpUrl = `${location.origin}/mcp`

  // Embed stats
  let embedTotal = 0
  let embedVectorized = 0
  let rebuilding = false
  let rebuildTimer = null

  onMount(async () => {
    await loadConfig()
    await loadMemories()
    await refreshStats()
  })

  onDestroy(() => {
    if (animFrame) cancelAnimationFrame(animFrame)
    if (rebuildTimer) clearInterval(rebuildTimer)
  })

  async function loadConfig() {
    try {
      const cfg = await getConfig()
      mcpEnabled = cfg.mcp_enabled ?? false
      memInject = cfg.memory_inject ?? false
    } catch {}
  }

  async function toggleMCP() {
    try {
      const cfg = await getConfig()
      cfg.mcp_enabled = !mcpEnabled
      await saveConfig(cfg)
      mcpEnabled = cfg.mcp_enabled
    } catch (e) {
      error = e.message
    }
  }

  async function toggleInject() {
    try {
      const cfg = await getConfig()
      cfg.memory_inject = !memInject
      await saveConfig(cfg)
      memInject = cfg.memory_inject
    } catch (e) {
      error = e.message
    }
  }

  async function loadMemories() {
    try {
      memories = await listMemories(50, 0)
    } catch (e) {
      error = e.message
    }
  }

  async function doSearch() {
    if (!searchQ.trim()) { searchScores = {}; await loadMemories(); return }
    searching = true
    try {
      if (semanticMode) {
        const results = await semanticSearchMemories(searchQ)
        searchScores = {}
        memories = results.map(r => { searchScores[r.id] = r.score; return r })
      } else {
        searchScores = {}
        memories = await searchMemories(searchQ)
      }
    } catch (e) {
      error = e.message
    } finally {
      searching = false
    }
  }

  async function refreshStats() {
    try {
      const s = await memoryStats()
      embedTotal = s.total
      embedVectorized = s.vectorized
    } catch {}
  }

  async function doRebuildEmbeddings() {
    try {
      await rebuildEmbeddings()
      rebuilding = true
      if (rebuildTimer) clearInterval(rebuildTimer)
      rebuildTimer = setInterval(async () => {
        const prev = embedVectorized
        await refreshStats()
        if (embedVectorized >= embedTotal && embedTotal > 0) {
          rebuilding = false
          clearInterval(rebuildTimer)
          rebuildTimer = null
        }
      }, 1500)
    } catch (e) {
      error = e.message
    }
  }

  function selectMemory(m) {
    selected = selected?.id === m.id ? null : m
    if (selected) {
      relFromID = selected.id
    }
  }

  async function deleteSelected() {
    if (!selected) return
    try {
      await deleteMemory(selected.id)
      memories = memories.filter(m => m.id !== selected.id)
      selected = null
    } catch (e) {
      error = e.message
    }
  }

  async function submitAdd() {
    addError = null; addOk = false
    const content = addContent.trim() || convertPreview.trim()
    if (!content) { addError = 'content required'; return }
    try {
      const tags = addTags.split(',').map(t => t.trim()).filter(Boolean)
      const m = await addMemory(content, addSource, tags)
      memories = [m, ...memories]
      addContent = ''; addSource = ''; addTags = ''; convertPreview = ''; convertFilename = ''
      addOk = true
      setTimeout(() => addOk = false, 2000)
    } catch (e) {
      addError = e.message
    }
  }

  async function submitRelate() {
    relError = null; relOk = false
    try {
      await relateMemories(relFromID, relToID, relType)
      relOk = true
      setTimeout(() => relOk = false, 2000)
    } catch (e) {
      relError = e.message
    }
  }

  // File drop
  function onDragOver(e) {
    e.preventDefault()
    draggingOver = true
  }
  function onDragLeave() { draggingOver = false }
  async function onDrop(e) {
    e.preventDefault()
    draggingOver = false
    const file = e.dataTransfer.files[0]
    if (!file) return
    converting = true
    addError = null
    try {
      const result = await convertFile(file)
      convertPreview = result.markdown
      convertFilename = result.filename
      addSource = result.filename
    } catch (e) {
      addError = e.message
    } finally {
      converting = false
    }
  }
  async function onFileInput(e) {
    const file = e.target.files[0]
    if (!file) return
    converting = true
    addError = null
    try {
      const result = await convertFile(file)
      convertPreview = result.markdown
      convertFilename = result.filename
      addSource = result.filename
    } catch (e) {
      addError = e.message
    } finally {
      converting = false
    }
  }

  // Graph
  async function loadGraph() {
    if (animFrame) cancelAnimationFrame(animFrame)
    try {
      const data = await getMemoryGraph()
      graphEdges = data.edges ?? []
      const w = canvas?.width ?? 600, h = canvas?.height ?? 400
      simNodes = (data.nodes ?? []).map(n => ({
        ...n,
        x: w/2 + (Math.random()-0.5)*300,
        y: h/2 + (Math.random()-0.5)*300,
        vx: 0, vy: 0,
      }))
      graphSelected = null
      animLoop()
    } catch (e) {
      error = e.message
    }
  }

  function animLoop() {
    tick()
    drawGraph()
    animFrame = requestAnimationFrame(animLoop)
  }

  function tick() {
    const map = {}
    simNodes.forEach(n => map[n.id] = n)

    for (let i = 0; i < simNodes.length; i++) {
      for (let j = i+1; j < simNodes.length; j++) {
        const a = simNodes[i], b = simNodes[j]
        const dx = (b.x - a.x) || 0.1
        const dy = (b.y - a.y) || 0.1
        const d2 = dx*dx + dy*dy
        const d = Math.sqrt(d2) || 1
        const f = 6000 / d2
        a.vx -= f*dx/d; a.vy -= f*dy/d
        b.vx += f*dx/d; b.vy += f*dy/d
      }
    }

    graphEdges.forEach(e => {
      const a = map[e.from_id], b = map[e.to_id]
      if (!a || !b) return
      const dx = b.x - a.x, dy = b.y - a.y
      const d = Math.sqrt(dx*dx + dy*dy) || 1
      const f = 0.008 * (d - 130) / d
      a.vx += f*dx; a.vy += f*dy
      b.vx -= f*dx; b.vy -= f*dy
    })

    const cx = (canvas?.width ?? 600)/2
    const cy = (canvas?.height ?? 400)/2
    simNodes.forEach(n => {
      if (n === draggingNode) return
      n.vx += (cx - n.x) * 0.001
      n.vy += (cy - n.y) * 0.001
      n.vx *= 0.88; n.vy *= 0.88
      n.x += n.vx; n.y += n.vy
    })
  }

  function cssVar(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  }

  function drawGraph() {
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    const edgeColor = cssVar('--graph-edge')
    const labelColor = cssVar('--graph-label')
    const nodeBg = cssVar('--graph-node')
    const nodeSel = cssVar('--graph-node-sel')
    const nodeBorder = cssVar('--graph-node-border')
    const nodeText = cssVar('--graph-node-text')
    const accent = cssVar('--accent')
    const map = {}
    simNodes.forEach(n => map[n.id] = n)

    graphEdges.forEach(e => {
      const a = map[e.from_id], b = map[e.to_id]
      if (!a || !b) return
      ctx.beginPath()
      ctx.moveTo(a.x, a.y)
      ctx.lineTo(b.x, b.y)
      ctx.strokeStyle = edgeColor
      ctx.lineWidth = 1.5
      ctx.stroke()
      ctx.fillStyle = labelColor
      ctx.font = '9px monospace'
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillText(e.rel_type, (a.x+b.x)/2, (a.y+b.y)/2 - 6)
    })

    simNodes.forEach(n => {
      const r = 20
      const isSelected = graphSelected?.id === n.id
      ctx.beginPath()
      ctx.arc(n.x, n.y, r, 0, Math.PI*2)
      ctx.fillStyle = isSelected ? nodeSel : nodeBg
      ctx.fill()
      ctx.strokeStyle = isSelected ? accent : nodeBorder
      ctx.lineWidth = isSelected ? 2 : 1
      ctx.stroke()

      const label = n.content.length > 18 ? n.content.slice(0,17)+'…' : n.content
      ctx.fillStyle = isSelected ? accent : nodeText
      ctx.font = '8px monospace'
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillText(label, n.x, n.y)
    })
  }

  function graphMouseDown(e) {
    const {x, y} = canvasPos(e)
    for (const n of simNodes) {
      if (dist(n.x, n.y, x, y) < 22) { draggingNode = n; return }
    }
    draggingNode = null
  }

  function graphMouseMove(e) {
    if (!draggingNode) return
    const {x, y} = canvasPos(e)
    draggingNode.x = x; draggingNode.y = y
    draggingNode.vx = 0; draggingNode.vy = 0
  }

  function graphMouseUp(e) {
    const {x, y} = canvasPos(e)
    if (draggingNode && dist(draggingNode.x, draggingNode.y, x, y) < 5) {
      graphSelected = draggingNode
    }
    draggingNode = null
  }

  function canvasPos(e) {
    const rect = canvas.getBoundingClientRect()
    return { x: e.clientX - rect.left, y: e.clientY - rect.top }
  }

  function dist(x1, y1, x2, y2) {
    return Math.sqrt((x2-x1)**2 + (y2-y1)**2)
  }

  function onTabChange(t) {
    tab = t
    if (t === 'graph') {
      setTimeout(() => {
        if (canvas) {
          canvas.width = canvas.offsetWidth
          canvas.height = canvas.offsetHeight
        }
        loadGraph()
      }, 50)
    }
  }

  function fmt(iso) {
    return new Date(iso).toLocaleDateString()
  }
</script>

<div class="pane">
  <!-- Status bars -->
  <div class="mcp-bar">
    <span class="mcp-dot" class:on={mcpEnabled}></span>
    <span class="mcp-label">MCP {mcpEnabled ? 'enabled' : 'disabled'}</span>
    {#if mcpEnabled}
      <code class="mcp-url">{mcpUrl}</code>
    {/if}
    <button class="toggle-btn" on:click={toggleMCP}>{mcpEnabled ? 'disable' : 'enable'}</button>
  </div>
  <div class="mcp-bar inject-bar">
    <span class="mcp-dot" class:on={memInject}></span>
    <span class="mcp-label">Auto-inject {memInject ? 'on' : 'off'}</span>
    <span class="inject-hint">— prepends relevant memories to every chat request</span>
    <span class="embed-stats" class:warn={embedVectorized < embedTotal}>{embedVectorized}/{embedTotal} embedded</span>
    <button class="rebuild-btn" class:rebuilding on:click={doRebuildEmbeddings} disabled={rebuilding} title="embed all un-indexed memories">
      {rebuilding ? 'embedding…' : 'embed ↺'}
    </button>
    <button class="toggle-btn" on:click={toggleInject}>{memInject ? 'disable' : 'enable'}</button>
  </div>

  {#if error}
    <div class="err">{error} <button class="dismiss" on:click={() => error=null}>×</button></div>
  {/if}

  <!-- Sub-tabs -->
  <div class="subtabs">
    <button class:active={tab==='memories'} on:click={() => onTabChange('memories')}>Memories</button>
    <button class:active={tab==='graph'} on:click={() => onTabChange('graph')}>Graph</button>
    <button class:active={tab==='add'} on:click={() => onTabChange('add')}>Add</button>
  </div>

  <!-- Memories tab -->
  {#if tab === 'memories'}
    <div class="search-row">
      <input
        bind:value={searchQ}
        placeholder="search memories..."
        on:input={doSearch}
      />
      <button
        class="sem-btn"
        class:sem-on={semanticMode}
        title={semanticMode ? 'semantic search (click to switch to keyword)' : 'keyword search (click to switch to semantic)'}
        on:click={() => { semanticMode = !semanticMode; doSearch() }}
      >∿</button>
      <button on:click={loadMemories}>↺</button>
    </div>

    <div class="list">
      {#if memories.length === 0}
        <div class="empty">no memories yet</div>
      {/if}
      {#each memories as m}
        <div
          class="mem-item"
          class:active={selected?.id === m.id}
          role="button"
          tabindex="0"
          on:click={() => selectMemory(m)}
          on:keydown={(e) => e.key === 'Enter' && selectMemory(m)}
        >
          <div class="mem-preview">{m.content.slice(0, 80)}{m.content.length > 80 ? '…' : ''}</div>
          <div class="mem-meta">
            <span class="mem-id">{m.id.slice(0,8)}</span>
            {#if searchScores[m.id] != null}
              <span class="score">{(searchScores[m.id] * 100).toFixed(0)}%</span>
            {/if}
            {#if m.source}<span class="mem-source">{m.source}</span>{/if}
            <span class="mem-date">{fmt(m.created_at)}</span>
            {#each m.tags as t}<span class="tag">{t}</span>{/each}
          </div>
        </div>
      {/each}
    </div>

    {#if selected}
      <div class="detail">
        <div class="detail-header">
          <span class="mem-id">{selected.id}</span>
          <button class="del-btn" on:click={deleteSelected}>delete</button>
        </div>
        <pre class="detail-content">{selected.content}</pre>
        {#if selected.source}<div class="detail-source">source: {selected.source}</div>{/if}

        <div class="relate-inline">
          <span class="section-label">relate to</span>
          <input bind:value={relToID} placeholder="other memory id" class="small-input" />
          <input bind:value={relType} placeholder="rel type" class="small-input small" />
          <button on:click={submitRelate} class="small-btn">link</button>
          {#if relOk}<span class="ok">linked</span>{/if}
          {#if relError}<span class="err-inline">{relError}</span>{/if}
        </div>
      </div>
    {/if}
  {/if}

  <!-- Graph tab -->
  {#if tab === 'graph'}
    <div class="graph-wrap">
      <canvas
        bind:this={canvas}
        on:mousedown={graphMouseDown}
        on:mousemove={graphMouseMove}
        on:mouseup={graphMouseUp}
      ></canvas>
      {#if graphSelected}
        <div class="graph-detail">
          <div class="mem-id">{graphSelected.id}</div>
          <div class="graph-detail-content">{graphSelected.content}</div>
          {#if graphSelected.source}<div class="detail-source">source: {graphSelected.source}</div>{/if}
        </div>
      {/if}
    </div>
    {#if simNodes.length === 0}
      <div class="empty">no memories to visualize</div>
    {/if}
  {/if}

  <!-- Add tab -->
  {#if tab === 'add'}
    <div class="add-form">
      <textarea
        bind:value={addContent}
        placeholder="memory content (or drop a file below to auto-convert)"
        rows="5"
      ></textarea>

      <div
        class="drop-zone"
        class:dragging={draggingOver}
        role="button"
        tabindex="0"
        on:dragover={onDragOver}
        on:dragleave={onDragLeave}
        on:drop={onDrop}
        on:click={() => document.getElementById('file-input').click()}
        on:keydown={(e) => e.key === 'Enter' && document.getElementById('file-input').click()}
      >
        {#if converting}
          converting…
        {:else if convertFilename}
          converted: <strong>{convertFilename}</strong> — edit below or save as-is
        {:else}
          drop file to convert (md, txt, pdf, docx, html) or click to browse
        {/if}
      </div>
      <input type="file" id="file-input" style="display:none" on:change={onFileInput} />

      {#if convertPreview}
        <div class="section-label">preview</div>
        <textarea bind:value={convertPreview} rows="8" class="preview-area"></textarea>
        <button class="small-btn" on:click={() => { addContent = convertPreview; convertPreview = '' }}>
          use as content
        </button>
      {/if}

      <div class="meta-row">
        <input bind:value={addSource} placeholder="source (optional)" />
        <input bind:value={addTags} placeholder="tags (comma separated)" />
      </div>

      <button class="save-btn" on:click={submitAdd}>Save Memory</button>
      {#if addOk}<div class="ok">saved!</div>{/if}
      {#if addError}<div class="err">{addError}</div>{/if}
    </div>
  {/if}
</div>

<style>
  .pane { display: flex; flex-direction: column; height: 100%; overflow: hidden; }

  .mcp-bar {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.4rem 0.75rem; background: var(--bg-elev); border-bottom: 1px solid var(--border-faint);
    flex-shrink: 0;
  }
  .mcp-dot {
    width: 8px; height: 8px; border-radius: 50%; background: var(--fg-6); flex-shrink: 0;
  }
  .mcp-dot.on { background: var(--success); box-shadow: 0 0 6px var(--success); }
  .mcp-label { font-size: 0.8rem; color: var(--fg-4); }
  .mcp-url { font-size: 0.75rem; color: var(--accent); background: var(--bg-accent); padding: 0.15rem 0.4rem; border-radius: 3px; }
.inject-bar { border-top: none; }
  .inject-hint { color: var(--fg-6); font-size: 0.75rem; }
  .toggle-btn {
    margin-left: auto; padding: 0.2rem 0.75rem; background: none; border: 1px solid var(--border);
    color: var(--fg-3); cursor: pointer; font-family: monospace; font-size: 0.8rem; border-radius: 3px;
  }
  .toggle-btn:hover { color: var(--accent); border-color: var(--accent); }

  .subtabs {
    display: flex; border-bottom: 1px solid var(--border-faint); flex-shrink: 0;
  }
  .subtabs button {
    padding: 0.4rem 1rem; background: none; border: none; border-bottom: 2px solid transparent;
    color: var(--fg-5); cursor: pointer; font-family: monospace; font-size: 0.85rem; margin-bottom: -1px;
  }
  .subtabs button.active { color: var(--accent); border-bottom-color: var(--accent); }

  .search-row {
    display: flex; gap: 0.4rem; padding: 0.5rem 0.75rem; flex-shrink: 0;
  }
  .search-row input {
    flex: 1; padding: 0.35rem 0.6rem; background: var(--bg-card); border: 1px solid var(--border-soft);
    color: var(--fg); font-family: monospace; font-size: 0.85rem; border-radius: 3px;
  }
  .search-row button {
    padding: 0.35rem 0.6rem; background: none; border: 1px solid var(--border-soft);
    color: var(--fg-5); cursor: pointer; border-radius: 3px;
  }
  .sem-btn { font-size: 1rem; }
  .sem-btn.sem-on { color: var(--accent); border-color: var(--accent); }

  .list { flex: 1; overflow-y: auto; padding: 0 0.5rem 0.5rem; }
  .empty { color: var(--fg-6); font-size: 0.85rem; padding: 1rem; text-align: center; }

  .mem-item {
    padding: 0.5rem 0.6rem; margin-bottom: 0.3rem; border: 1px solid var(--border-faint);
    border-radius: 4px; cursor: pointer; background: var(--bg);
  }
  .mem-item:hover { border-color: var(--graph-edge); }
  .mem-item.active { border-color: var(--accent); background: var(--bg-accent); }
  .mem-preview { color: var(--fg-1); font-size: 0.85rem; margin-bottom: 0.25rem; }
  .mem-meta { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  .mem-id { color: var(--graph-label); font-size: 0.75rem; font-family: monospace; }
  .mem-source { color: var(--success-dim); font-size: 0.75rem; }
  .mem-date { color: var(--fg-5); font-size: 0.75rem; margin-left: auto; }
  .tag { background: var(--tag-bg); color: var(--tag-fg); font-size: 0.7rem; padding: 0.1rem 0.3rem; border-radius: 2px; }
  .score { color: var(--accent); font-size: 0.72rem; font-family: monospace; }
  .embed-stats { font-size: 0.75rem; color: var(--graph-label); font-family: monospace; }
  .embed-stats.warn { color: var(--warn); }
  .rebuild-btn {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid var(--border);
    color: var(--fg-3); cursor: pointer; font-family: monospace; font-size: 0.75rem; border-radius: 3px;
  }
  .rebuild-btn:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
  .rebuild-btn:disabled { opacity: 0.5; cursor: default; }
  .rebuild-btn.rebuilding { color: var(--warn); border-color: var(--warn); animation: pulse 1s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1 } 50% { opacity: 0.5 } }

  .detail {
    border-top: 1px solid var(--border-faint); padding: 0.6rem 0.75rem; flex-shrink: 0;
    max-height: 40%; overflow-y: auto;
  }
  .detail-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.4rem; }
  .detail-content {
    white-space: pre-wrap; word-break: break-word; font-size: 0.85rem;
    color: var(--fg-1); margin: 0 0 0.5rem; font-family: monospace; line-height: 1.5;
  }
  .detail-source { color: var(--success-dim); font-size: 0.8rem; margin-bottom: 0.5rem; }
  .del-btn {
    padding: 0.2rem 0.6rem; background: none; border: 1px solid var(--error);
    color: var(--error); cursor: pointer; font-size: 0.8rem; border-radius: 3px;
  }
  .del-btn:hover { background: var(--error-bg); }

  .relate-inline {
    display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap;
    padding-top: 0.4rem; border-top: 1px solid var(--border-faint); margin-top: 0.4rem;
  }
  .section-label { color: var(--fg-5); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .small-input {
    padding: 0.25rem 0.5rem; background: var(--bg-card); border: 1px solid var(--border-soft);
    color: var(--fg); font-family: monospace; font-size: 0.8rem; border-radius: 3px; flex: 1;
  }
  .small-input.small { flex: 0 0 90px; }
  .small-btn {
    padding: 0.25rem 0.6rem; background: none; border: 1px solid var(--tag-border);
    color: var(--success-dim); cursor: pointer; font-family: monospace; font-size: 0.8rem; border-radius: 3px;
  }
  .small-btn:hover { background: var(--tag-bg); }
  .ok { color: var(--success); font-size: 0.8rem; }
  .err-inline { color: var(--error); font-size: 0.8rem; }

  /* Graph */
  .graph-wrap { flex: 1; display: flex; flex-direction: column; overflow: hidden; position: relative; }
  canvas { flex: 1; width: 100%; display: block; cursor: grab; }
  canvas:active { cursor: grabbing; }
  .graph-detail {
    padding: 0.6rem 0.75rem; border-top: 1px solid var(--border-faint);
    max-height: 120px; overflow-y: auto; flex-shrink: 0;
  }
  .graph-detail-content { font-size: 0.85rem; color: var(--fg-1); margin-top: 0.25rem; white-space: pre-wrap; }

  /* Add form */
  .add-form {
    padding: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;
    overflow-y: auto; flex: 1;
  }
  textarea {
    background: var(--bg-card); border: 1px solid var(--border-soft); color: var(--fg);
    font-family: monospace; font-size: 0.85rem; border-radius: 4px;
    padding: 0.5rem; resize: vertical;
  }
  .drop-zone {
    border: 2px dashed var(--border-soft); border-radius: 4px; padding: 1.25rem;
    text-align: center; color: var(--fg-6); cursor: pointer; font-size: 0.85rem;
    transition: border-color 0.15s, color 0.15s;
  }
  .drop-zone:hover { border-color: var(--border); color: var(--fg-4); }
  .drop-zone.dragging { border-color: var(--accent); color: var(--accent); }
  .preview-area { font-size: 0.8rem; }
  .meta-row { display: flex; gap: 0.5rem; }
  .meta-row input {
    flex: 1; padding: 0.35rem 0.6rem; background: var(--bg-card); border: 1px solid var(--border-soft);
    color: var(--fg); font-family: monospace; font-size: 0.85rem; border-radius: 3px;
  }
  .save-btn {
    padding: 0.5rem 1.25rem; background: var(--accent); color: var(--accent-fg);
    border: none; border-radius: 4px; cursor: pointer;
    font-weight: bold; align-self: flex-start; font-family: monospace;
  }
  .err {
    color: var(--error); font-size: 0.85rem; padding: 0.4rem 0.75rem;
    background: var(--error-bg); border-bottom: 1px solid var(--error-border);
    display: flex; justify-content: space-between; align-items: center;
  }
  .dismiss { background: none; border: none; color: var(--error); cursor: pointer; }
</style>
