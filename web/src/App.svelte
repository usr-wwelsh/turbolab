<script>
  import { onMount } from 'svelte'
  import StatusBar from './lib/StatusBar.svelte'
  import ModelSearch from './lib/ModelSearch.svelte'
  import Chat from './lib/Chat.svelte'
  import Settings from './lib/Settings.svelte'
  import LogPanel from './lib/LogPanel.svelte'
  import Usage from './lib/Usage.svelte'
  import Memory from './lib/Memory.svelte'
  import { getStatus } from './lib/api.js'
  import { initTheme } from './lib/theme.js'

  let tab = 'chat'
  let status = null
  let logLines = []
  let crashed = false
  let showLogs = false
  let inferring = false

  let sessions = [{ id: 1, name: 'Chat 1', messages: [] }]
  let activeId = 1
  let nextId = 2

  $: activeSession = sessions.find(s => s.id === activeId)

  async function refreshStatus() {
    try {
      const s = await getStatus()
      if (s.loading && !status?.loading) showLogs = true // auto-open logs when download starts
      status = s
    } catch {}
  }

  onMount(() => {
    initTheme()
    refreshStatus()
    const interval = setInterval(refreshStatus, 3000)

    const es = new EventSource('/api/events')
    es.onmessage = (e) => {
      const line = e.data
      logLines = [...logLines.slice(-499), line]
      if (line.includes('crashed')) {
        crashed = true
      }
    }

    return () => {
      clearInterval(interval)
      es.close()
    }
  })

  function onModelLoaded(modelId) {
    tab = 'chat'
    refreshStatus()
  }

  function newSession() {
    const s = { id: nextId++, name: `Chat ${nextId - 1}`, messages: [] }
    sessions = [...sessions, s]
    activeId = s.id
  }

  function clearSession() {
    sessions = sessions.map(s => s.id === activeId ? { ...s, messages: [] } : s)
  }

  function switchSession(id) {
    activeId = id
  }

  function closeSession(id) {
    if (sessions.length === 1) { clearSession(); return }
    const idx = sessions.findIndex(s => s.id === id)
    sessions = sessions.filter(s => s.id !== id)
    if (activeId === id) activeId = sessions[Math.min(idx, sessions.length - 1)].id
  }
</script>

<div class="app">
  <StatusBar {status} {crashed} {inferring} onShowLogs={() => { showLogs = true; crashed = false }} />
  <div class="tabs">
    <button class:active={tab === 'chat'} on:click={() => tab = 'chat'}>Chat</button>
    <button class:active={tab === 'models'} on:click={() => tab = 'models'}>Models</button>
    <button class:active={tab === 'memory'} on:click={() => tab = 'memory'}>Memory</button>
    <button class:active={tab === 'usage'} on:click={() => tab = 'usage'}>Usage</button>
    <button class:active={tab === 'settings'} on:click={() => tab = 'settings'}>Settings</button>
  </div>
  {#if tab === 'chat'}
    <div class="session-bar">
      {#each sessions as s}
        <button class="session-tab" class:active={s.id === activeId} on:click={() => switchSession(s.id)}>
          {s.name}
          {#if sessions.length > 1}
            <button class="close" on:click|stopPropagation={() => closeSession(s.id)}>×</button>
          {/if}
        </button>
      {/each}
      <button class="new-session" on:click={newSession} title="New chat">+</button>
      <button class="clear-session" on:click={clearSession} title="Clear context">clear</button>
    </div>
  {/if}
  {#if showLogs}
    <LogPanel lines={logLines} onClose={() => showLogs = false} />
  {/if}
  <div class="content">
    {#if tab === 'chat'}
      {#each sessions as s (s.id)}
        <div style="height:100%;display:{s.id===activeId?'flex':'none'};flex-direction:column;">
          <Chat modelRunning={status?.running ?? false} model={status?.model ?? ''} memInject={status?.memory_inject ?? false} bind:messages={s.messages} sessionId={s.id} onStreaming={(v) => inferring = v} />
        </div>
      {/each}
    {:else if tab === 'models'}
      <ModelSearch onLoad={onModelLoaded} />
    {:else if tab === 'memory'}
      <Memory />
    {:else if tab === 'usage'}
      <Usage />
    {:else}
      <Settings />
    {/if}
  </div>
  <nav class="mobile-nav">
    <button class:active={tab === 'chat'} on:click={() => tab = 'chat'}>
      <span class="nav-icon">▣</span><span class="nav-label">Chat</span>
    </button>
    <button class:active={tab === 'models'} on:click={() => tab = 'models'}>
      <span class="nav-icon">⬇</span><span class="nav-label">Models</span>
    </button>
    <button class:active={tab === 'memory'} on:click={() => tab = 'memory'}>
      <span class="nav-icon">◈</span><span class="nav-label">Memory</span>
    </button>
    <button class:active={tab === 'usage'} on:click={() => tab = 'usage'}>
      <span class="nav-icon">▤</span><span class="nav-label">Usage</span>
    </button>
    <button class:active={tab === 'settings'} on:click={() => tab = 'settings'}>
      <span class="nav-icon">⚙</span><span class="nav-label">Settings</span>
    </button>
  </nav>
</div>

<style>
  :global(:root) {
    --bg: #0d0d0d;
    --bg-elev: #111;
    --bg-card: #1a1a1a;
    --bg-deep: #0a0a0a;
    --bg-accent: #0d1a22;
    --border: #333;
    --border-soft: #2a2a2a;
    --border-subtle: #222;
    --border-faint: #1e1e1e;
    --fg: #eee;
    --fg-1: #ccc;
    --fg-2: #aaa;
    --fg-3: #888;
    --fg-4: #666;
    --fg-5: #555;
    --fg-6: #444;
    --accent: #7cf;
    --accent-fg: #000;
    --success: #4f4;
    --success-dim: #6a6;
    --warn: #fa0;
    --warn-bg: #1a1200;
    --error: #f66;
    --error-bg: #1a0808;
    --error-border: #2a1212;
    --purple: #a7f;
    --tag-bg: #1a2a1a;
    --tag-fg: #6a6;
    --tag-border: #2a4a2a;
    --inject-fg: #5a8a6a;
    --inject-fg-dim: #6a8a7a;
    --inject-border: #1a3a2a;
    --inject-id-fg: #2a4a3a;
    --graph-edge: #2a3a4a;
    --graph-label: #445;
    --graph-node: #111827;
    --graph-node-sel: #0d2233;
    --graph-node-border: #2a4a6a;
    --graph-node-text: #9ab;
    --chart-input: #2a5fc4bb;
    --chart-output: #55ccff99;
    --stat-accent-bg: #141408;
    --stat-accent-border: #2a2a14;
    --stat-accent-fg: #cf4;
    --rate-note: #383838;
  }
  :global(:root[data-theme="light"]) {
    --bg: #fafafa;
    --bg-elev: #f0f0f0;
    --bg-card: #fff;
    --bg-deep: #f5f5f5;
    --bg-accent: #e0f0fa;
    --border: #bbb;
    --border-soft: #ccc;
    --border-subtle: #d5d5d5;
    --border-faint: #e5e5e5;
    --fg: #1a1a1a;
    --fg-1: #333;
    --fg-2: #444;
    --fg-3: #555;
    --fg-4: #777;
    --fg-5: #888;
    --fg-6: #aaa;
    --accent: #06c;
    --accent-fg: #fff;
    --success: #060;
    --success-dim: #390;
    --warn: #c70;
    --warn-bg: #fff8e0;
    --error: #c33;
    --error-bg: #fde8e8;
    --error-border: #f5b0b0;
    --purple: #639;
    --tag-bg: #e8f5e8;
    --tag-fg: #060;
    --tag-border: #b0d8b0;
    --inject-fg: #060;
    --inject-fg-dim: #283;
    --inject-border: #b0d8b0;
    --inject-id-fg: #6a8a7a;
    --graph-edge: #a8b8c8;
    --graph-label: #889;
    --graph-node: #f0f4fa;
    --graph-node-sel: #e0f0fa;
    --graph-node-border: #88a8c8;
    --graph-node-text: #345;
    --chart-input: #2a5fc4cc;
    --chart-output: #66bbeecc;
    --stat-accent-bg: #f8f8e0;
    --stat-accent-border: #d8d890;
    --stat-accent-fg: #460;
    --rate-note: #aaa;
  }
  :global(*, *::before, *::after) { box-sizing: border-box; }
  :global(*) { -webkit-tap-highlight-color: transparent; }
  :global(body) {
    margin: 0; background: var(--bg); color: var(--fg);
    font-family: monospace; height: 100dvh;
    -webkit-text-size-adjust: 100%;
    overscroll-behavior: none;
  }
  .app {
    display: flex; flex-direction: column; height: 100dvh;
  }
  .mobile-nav { display: none; }
  .tabs {
    display: flex; gap: 0; border-bottom: 1px solid var(--border-subtle);
  }
  .tabs button {
    padding: 0.5rem 1.5rem; background: none; border: none;
    color: var(--fg-5); cursor: pointer; font-family: monospace; font-size: 0.9rem;
    border-bottom: 2px solid transparent; margin-bottom: -1px;
  }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }
  .session-bar {
    display: flex; align-items: center; gap: 2px;
    padding: 0.25rem 0.5rem; border-bottom: 1px solid var(--border-faint);
    background: var(--bg-elev); overflow-x: auto;
  }
  .session-tab {
    padding: 0.2rem 0.6rem; background: none; border: 1px solid var(--border);
    color: var(--fg-5); cursor: pointer; font-family: monospace; font-size: 0.8rem;
    border-radius: 3px; display: flex; align-items: center; gap: 0.3rem;
    white-space: nowrap;
  }
  .session-tab.active { color: var(--accent); border-color: var(--accent); background: var(--bg-accent); }
  .session-tab .close {
    color: var(--fg-6); font-size: 0.9rem; line-height: 1; padding: 0 0.1rem;
    background: none; border: none; cursor: pointer; font-family: monospace;
  }
  .session-tab .close:hover { color: var(--error); }
  .new-session {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid var(--border);
    color: var(--success); cursor: pointer; font-family: monospace; font-size: 0.9rem;
    border-radius: 3px; margin-left: 0.25rem;
  }
  .new-session:hover { border-color: var(--success); }
  .clear-session {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid var(--border);
    color: var(--fg-5); cursor: pointer; font-family: monospace; font-size: 0.75rem;
    border-radius: 3px; margin-left: auto;
  }
  .clear-session:hover { color: var(--error); border-color: var(--error); }
  .content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }

  @media (max-width: 640px) {
    .tabs { display: none; }
    .session-bar {
      padding: 0.35rem 0.5rem; gap: 0.35rem;
      scrollbar-width: none;
      user-select: none; -webkit-user-select: none;
    }
    .session-bar::-webkit-scrollbar { display: none; }
    .session-tab {
      padding: 0.35rem 0.7rem; font-size: 0.78rem;
      border-radius: 999px; border-color: var(--border-subtle);
    }
    .session-tab.active { background: var(--bg-accent); }
    .new-session {
      padding: 0.35rem 0.7rem; border-radius: 999px;
      font-size: 1rem; line-height: 1;
    }
    .clear-session {
      font-size: 0.72rem; padding: 0.35rem 0.7rem; border-radius: 999px;
    }

    .mobile-nav {
      display: flex; flex-shrink: 0;
      border-top: 1px solid var(--border-subtle);
      background: var(--bg-elev);
      padding-bottom: env(safe-area-inset-bottom);
      user-select: none; -webkit-user-select: none;
    }
    .mobile-nav button {
      flex: 1; background: none; border: none;
      color: var(--fg-5); cursor: pointer;
      font-family: monospace; font-size: 0.68rem;
      padding: 0.4rem 0.2rem 0.5rem;
      display: flex; flex-direction: column;
      align-items: center; gap: 0.15rem;
      border-top: 2px solid transparent; margin-top: -1px;
      transition: color 0.15s;
    }
    .mobile-nav button.active {
      color: var(--accent); border-top-color: var(--accent);
    }
    .nav-icon { font-size: 1.05rem; line-height: 1; }
    .nav-label { font-size: 0.65rem; letter-spacing: 0.02em; }
  }
</style>
