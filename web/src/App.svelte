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
      <Memory {status} />
    {:else if tab === 'usage'}
      <Usage />
    {:else}
      <Settings />
    {/if}
  </div>
</div>

<style>
  :global(*, *::before, *::after) { box-sizing: border-box; }
  :global(body) {
    margin: 0; background: #0d0d0d; color: #eee;
    font-family: monospace; height: 100vh;
  }
  .app { display: flex; flex-direction: column; height: 100vh; }
  .tabs {
    display: flex; gap: 0; border-bottom: 1px solid #222;
  }
  .tabs button {
    padding: 0.5rem 1.5rem; background: none; border: none;
    color: #555; cursor: pointer; font-family: monospace; font-size: 0.9rem;
    border-bottom: 2px solid transparent; margin-bottom: -1px;
  }
  .tabs button.active { color: #7cf; border-bottom-color: #7cf; }
  .session-bar {
    display: flex; align-items: center; gap: 2px;
    padding: 0.25rem 0.5rem; border-bottom: 1px solid #1a1a1a;
    background: #111; overflow-x: auto;
  }
  .session-tab {
    padding: 0.2rem 0.6rem; background: none; border: 1px solid #333;
    color: #555; cursor: pointer; font-family: monospace; font-size: 0.8rem;
    border-radius: 3px; display: flex; align-items: center; gap: 0.3rem;
    white-space: nowrap;
  }
  .session-tab.active { color: #7cf; border-color: #7cf; background: #0d1a22; }
  .session-tab .close {
    color: #444; font-size: 0.9rem; line-height: 1; padding: 0 0.1rem;
    background: none; border: none; cursor: pointer; font-family: monospace;
  }
  .session-tab .close:hover { color: #f66; }
  .new-session {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid #333;
    color: #4f4; cursor: pointer; font-family: monospace; font-size: 0.9rem;
    border-radius: 3px; margin-left: 0.25rem;
  }
  .new-session:hover { border-color: #4f4; }
  .clear-session {
    padding: 0.2rem 0.5rem; background: none; border: 1px solid #333;
    color: #555; cursor: pointer; font-family: monospace; font-size: 0.75rem;
    border-radius: 3px; margin-left: auto;
  }
  .clear-session:hover { color: #f66; border-color: #f66; }
  .content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
</style>
