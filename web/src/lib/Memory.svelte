<script>
  import { onMount, onDestroy } from 'svelte'
  import {
    listMemories, searchMemories, semanticSearchMemories, rebuildEmbeddings,
    addMemory, deleteMemory, relateMemories, unrelateMemories, getMemoryGraph,
    convertFile, getConfig, saveConfig
  } from './api.js'

  export let status = null

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

  onMount(async () => {
    await loadConfig()
    await loadMemories()
  })

  onDestroy(() => {
    if (animFrame) cancelAnimationFrame(animFrame)
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

  async function doRebuildEmbeddings() {
    try {
      await rebuildEmbeddings()
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

  function drawGraph() {
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    const map = {}
    simNodes.forEach(n => map[n.id] = n)

    graphEdges.forEach(e => {
      const a = map[e.from_id], b = map[e.to_id]
      if (!a || !b) return
      ctx.beginPath()
      ctx.moveTo(a.x, a.y)
      ctx.lineTo(b.x, b.y)
      ctx.strokeStyle = '#2a3a4a'
      ctx.lineWidth = 1.5
      ctx.stroke()
      ctx.fillStyle = '#445'
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
      ctx.fillStyle = isSelected ? '#0d2233' : '#111827'
      ctx.fill()
      ctx.strokeStyle = isSelected ? '#7cf' : '#2a4a6a'
      ctx.lineWidth = isSelected ? 2 : 1
      ctx.stroke()

      const label = n.content.length > 18 ? n.content.slice(0,17)+'…' : n.content
      ctx.fillStyle = isSelected ? '#7cf' : '#9ab'
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
    <button class="rebuild-btn" on:click={doRebuildEmbeddings} title="embed all un-indexed memories">embed ↺</button>
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
          on:click={() => selectMemory(m)}
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
        on:dragover={onDragOver}
        on:dragleave={onDragLeave}
        on:drop={onDrop}
        on:click={() => document.getElementById('file-input').click()}
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
    padding: 0.4rem 0.75rem; background: #111; border-bottom: 1px solid #1e1e1e;
    flex-shrink: 0;
  }
  .mcp-dot {
    width: 8px; height: 8px; border-radius: 50%; background: #444; flex-shrink: 0;
  }
  .mcp-dot.on { background: #4f4; box-shadow: 0 0 6px #4f4; }
  .mcp-label { font-size: 0.8rem; color: #666; }
  .mcp-url { font-size: 0.75rem; color: #7cf; background: #0d1a22; padding: 0.15rem 0.4rem; border-radius: 3px; }
.inject-bar { border-top: none; }
  .inject-hint { color: #444; font-size: 0.75rem; }
  .toggle-btn {
    margin-left: auto; padding: 0.2rem 0.75rem; background: none; border: 1px solid #333;
    color: #888; cursor: pointer; font-family: monospace; font-size: 0.8rem; border-radius: 3px;
  }
  .toggle-btn:hover { color: #7cf; border-color: #7cf; }

  .subtabs {
    display: flex; border-bottom: 1px solid #1e1e1e; flex-shrink: 0;
  }
  .subtabs button {
    padding: 0.4rem 1rem; background: none; border: none; border-bottom: 2px solid transparent;
    color: #555; cursor: pointer; font-family: monospace; font-size: 0.85rem; margin-bottom: -1px;
  }
  .subtabs button.active { color: #7cf; border-bottom-color: #7cf; }

  .search-row {
    display: flex; gap: 0.4rem; padding: 0.5rem 0.75rem; flex-shrink: 0;
  }
  .search-row input {
    flex: 1; padding: 0.35rem 0.6rem; background: #1a1a1a; border: 1px solid #2a2a2a;
    color: #eee; font-family: monospace; font-size: 0.85rem; border-radius: 3px;
  }
  .search-row button {
    padding: 0.35rem 0.6rem; background: none; border: 1px solid #2a2a2a;
    color: #555; cursor: pointer; border-radius: 3px;
  }
  .sem-btn { font-size: 1rem; }
  .sem-btn.sem-on { color: #7cf; border-color: #7cf; }

  .list { flex: 1; overflow-y: auto; padding: 0 0.5rem 0.5rem; }
  .empty { color: #444; font-size: 0.85rem; padding: 1rem; text-align: center; }

  .mem-item {
    padding: 0.5rem 0.6rem; margin-bottom: 0.3rem; border: 1px solid #1e1e1e;
    border-radius: 4px; cursor: pointer; background: #0d0d0d;
  }
  .mem-item:hover { border-color: #2a3a4a; }
  .mem-item.active { border-color: #7cf; background: #0d1a22; }
  .mem-preview { color: #ccc; font-size: 0.85rem; margin-bottom: 0.25rem; }
  .mem-meta { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  .mem-id { color: #446; font-size: 0.75rem; font-family: monospace; }
  .mem-source { color: #6a8; font-size: 0.75rem; }
  .mem-date { color: #555; font-size: 0.75rem; margin-left: auto; }
  .tag { background: #1a2a1a; color: #6a6; font-size: 0.7rem; padding: 0.1rem 0.3rem; border-radius: 2px; }
  .score { color: #7cf; font-size: 0.72rem; font-family: monospace; }
  .rebuild-btn {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid #1a2a3a;
    color: #4a7a9a; cursor: pointer; font-family: monospace; font-size: 0.75rem; border-radius: 3px;
  }
  .rebuild-btn:hover { color: #7cf; border-color: #7cf; }

  .detail {
    border-top: 1px solid #1e1e1e; padding: 0.6rem 0.75rem; flex-shrink: 0;
    max-height: 40%; overflow-y: auto;
  }
  .detail-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.4rem; }
  .detail-content {
    white-space: pre-wrap; word-break: break-word; font-size: 0.85rem;
    color: #ccc; margin: 0 0 0.5rem; font-family: monospace; line-height: 1.5;
  }
  .detail-source { color: #6a8; font-size: 0.8rem; margin-bottom: 0.5rem; }
  .del-btn {
    padding: 0.2rem 0.6rem; background: none; border: 1px solid #422;
    color: #f66; cursor: pointer; font-size: 0.8rem; border-radius: 3px;
  }
  .del-btn:hover { background: #1a0808; }

  .relate-inline {
    display: flex; align-items: center; gap: 0.4rem; flex-wrap: wrap;
    padding-top: 0.4rem; border-top: 1px solid #1a1a1a; margin-top: 0.4rem;
  }
  .section-label { color: #555; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .small-input {
    padding: 0.25rem 0.5rem; background: #1a1a1a; border: 1px solid #2a2a2a;
    color: #eee; font-family: monospace; font-size: 0.8rem; border-radius: 3px; flex: 1;
  }
  .small-input.small { flex: 0 0 90px; }
  .small-btn {
    padding: 0.25rem 0.6rem; background: none; border: 1px solid #2a4a2a;
    color: #6a6; cursor: pointer; font-family: monospace; font-size: 0.8rem; border-radius: 3px;
  }
  .small-btn:hover { background: #0d1a0d; }
  .ok { color: #4f4; font-size: 0.8rem; }
  .err-inline { color: #f66; font-size: 0.8rem; }

  /* Graph */
  .graph-wrap { flex: 1; display: flex; flex-direction: column; overflow: hidden; position: relative; }
  canvas { flex: 1; width: 100%; display: block; cursor: grab; }
  canvas:active { cursor: grabbing; }
  .graph-detail {
    padding: 0.6rem 0.75rem; border-top: 1px solid #1e1e1e;
    max-height: 120px; overflow-y: auto; flex-shrink: 0;
  }
  .graph-detail-content { font-size: 0.85rem; color: #ccc; margin-top: 0.25rem; white-space: pre-wrap; }

  /* Add form */
  .add-form {
    padding: 0.75rem; display: flex; flex-direction: column; gap: 0.5rem;
    overflow-y: auto; flex: 1;
  }
  textarea {
    background: #1a1a1a; border: 1px solid #2a2a2a; color: #eee;
    font-family: monospace; font-size: 0.85rem; border-radius: 4px;
    padding: 0.5rem; resize: vertical;
  }
  .drop-zone {
    border: 2px dashed #2a2a2a; border-radius: 4px; padding: 1.25rem;
    text-align: center; color: #444; cursor: pointer; font-size: 0.85rem;
    transition: border-color 0.15s, color 0.15s;
  }
  .drop-zone:hover { border-color: #3a3a3a; color: #666; }
  .drop-zone.dragging { border-color: #7cf; color: #7cf; }
  .preview-area { font-size: 0.8rem; }
  .meta-row { display: flex; gap: 0.5rem; }
  .meta-row input {
    flex: 1; padding: 0.35rem 0.6rem; background: #1a1a1a; border: 1px solid #2a2a2a;
    color: #eee; font-family: monospace; font-size: 0.85rem; border-radius: 3px;
  }
  .save-btn {
    padding: 0.5rem 1.25rem; background: #7cf; color: #000;
    border: none; border-radius: 4px; cursor: pointer;
    font-weight: bold; align-self: flex-start; font-family: monospace;
  }
  .err {
    color: #f66; font-size: 0.85rem; padding: 0.4rem 0.75rem;
    background: #1a0808; border-bottom: 1px solid #2a1212;
    display: flex; justify-content: space-between; align-items: center;
  }
  .dismiss { background: none; border: none; color: #f66; cursor: pointer; }
</style>
