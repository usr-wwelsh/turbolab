<script>
  import { chatStream, searchMemories } from './api.js'

  export let modelRunning = false
  export let model = ''
  export let memInject = false
  export let messages = []
  export const sessionId = 0
  export let onStreaming = (_) => {}

  let input = ''
  let streaming = false
  let gotTokens = false
  let abortCtrl = null
  let chatEl
  let historyIdx = -1
  let savedDraft = ''
  let textareaEl

  $: userHistory = messages.filter(m => m.role === 'user').map(m => m.content)

  async function send() {
    if (!input.trim() || streaming) return
    const userMsg = { role: 'user', content: input }
    messages = [...messages, userMsg]
    input = ''
    historyIdx = -1
    savedDraft = ''
    streaming = true
    gotTokens = false
    onStreaming(true)
    abortCtrl = new AbortController()

    const assistantMsg = { role: 'assistant', content: '', injected: null, injectedOpen: false }
    messages = [...messages, assistantMsg]
    scrollBottom()

    // Fire memory search in parallel with the stream — fast local query
    if (memInject) {
      searchMemories(userMsg.content, 3).then(mems => {
        if (mems?.length > 0) {
          assistantMsg.injected = mems
          messages = messages
        }
      }).catch(() => {})
    }

    try {
      await chatStream(messages.slice(0, -1), token => {
        gotTokens = true
        assistantMsg.content += token
        messages = messages
        scrollBottom()
      }, abortCtrl.signal, model)
      if (!gotTokens) {
        assistantMsg.content = 'No response — model returned empty stream (may have crashed)'
        assistantMsg.error = true
        messages = messages
      }
    } catch (e) {
      if (e.name !== 'AbortError') {
        assistantMsg.content = e.message
        assistantMsg.error = true
        assistantMsg.detail = e.detail ?? null
        messages = messages
      }
    }

    abortCtrl = null
    streaming = false
    onStreaming(false)
  }

  function onKey(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
      return
    }
    if (e.key === 'ArrowUp' && input === '' || (e.key === 'ArrowUp' && historyIdx !== -1)) {
      if (userHistory.length === 0) return
      if (historyIdx === -1) {
        savedDraft = input
        historyIdx = userHistory.length - 1
      } else if (historyIdx > 0) {
        historyIdx--
      }
      input = userHistory[historyIdx]
      e.preventDefault()
      setTimeout(() => textareaEl?.setSelectionRange(input.length, input.length), 0)
      return
    }
    if (e.key === 'ArrowDown' && historyIdx !== -1) {
      if (historyIdx < userHistory.length - 1) {
        historyIdx++
        input = userHistory[historyIdx]
      } else {
        historyIdx = -1
        input = savedDraft
      }
      e.preventDefault()
      setTimeout(() => textareaEl?.setSelectionRange(input.length, input.length), 0)
    }
  }

  function stop() {
    abortCtrl?.abort()
  }

  function scrollBottom() {
    chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' })
  }
</script>

<div class="chat">
  <div class="messages" bind:this={chatEl}>
    {#if messages.length === 0}
      <div class="empty">
        {modelRunning ? 'Start chatting.' : 'Load a model first.'}
      </div>
    {/if}
    {#each messages as msg}
      <div class="msg {msg.role}">
        <span class="role">{msg.role === 'user' ? 'you' : 'ai'}</span>
        <div class="content-wrap">
          {#if msg.injected?.length > 0}
            <div class="inject-badge">
              <button class="inject-toggle" on:click={() => { msg.injectedOpen = !msg.injectedOpen; messages = messages }}>
                ↑ {msg.injected.length} memor{msg.injected.length === 1 ? 'y' : 'ies'} injected {msg.injectedOpen ? '▾' : '▸'}
              </button>
              {#if msg.injectedOpen}
                <div class="inject-list">
                  {#each msg.injected as m}
                    <div class="inject-item">
                      <span class="inject-id">{m.id.slice(0,8)}</span>
                      <span class="inject-content">{m.content.length > 200 ? m.content.slice(0, 200) + '…' : m.content}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
          <pre class="content" class:error={msg.error}>{msg.content}{#if msg.role === 'assistant' && streaming && msg === messages[messages.length - 1]}<span class="cursor">▋</span>{/if}</pre>
          {#if msg.detail}
            <details class="error-detail">
              <summary>details</summary>
              <pre>{msg.detail}</pre>
            </details>
          {/if}
        </div>
      </div>
    {/each}
  </div>
  {#if streaming}
    <div class="generating">generating<span class="dots"><span>.</span><span>.</span><span>.</span></span></div>
  {/if}
  <div class="input-row">
    <textarea
      bind:this={textareaEl}
      bind:value={input}
      on:keydown={onKey}
      placeholder={modelRunning ? 'Message... (↑↓ history)' : 'Load a model to chat'}
      disabled={!modelRunning || streaming}
      rows="2"
    ></textarea>
    {#if streaming}
      <button class="stop-btn" on:click={stop}>Stop</button>
    {:else}
      <button on:click={send} disabled={!modelRunning}>Send</button>
    {/if}
  </div>
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; }
  .messages {
    flex: 1; overflow-y: auto; padding: 1rem;
    display: flex; flex-direction: column; gap: 1rem;
  }
  .empty { color: #444; font-family: monospace; text-align: center; margin-top: 4rem; }
  .msg { display: flex; gap: 0.75rem; }
  .role { color: #555; font-family: monospace; font-size: 0.8rem; min-width: 2rem; padding-top: 0.1rem; }
  .msg.user .role { color: #7cf; }
  .msg.assistant .role { color: #4f4; }
  .content-wrap { flex: 1; }
  .content {
    margin: 0; font-family: monospace; font-size: 0.9rem;
    color: #ddd; white-space: pre-wrap; word-break: break-word;
  }
  .content.error { color: #f66; }
  .cursor { animation: blink 1s step-end infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .error-detail { margin-top: 0.4rem; }
  .error-detail summary { color: #555; font-size: 0.8rem; cursor: pointer; }
  .error-detail pre { color: #888; font-size: 0.78rem; margin: 0.25rem 0 0; white-space: pre-wrap; }
  .generating {
    padding: 0.25rem 1rem; font-size: 0.75rem; color: #a7f;
    border-top: 1px solid #1a1a1a; font-family: monospace;
  }
  .dots span { animation: appear 1.2s infinite; opacity: 0; }
  .dots span:nth-child(2) { animation-delay: 0.4s; }
  .dots span:nth-child(3) { animation-delay: 0.8s; }
  @keyframes appear { 0%, 100% { opacity: 0; } 50% { opacity: 1; } }
  .input-row {
    display: flex; gap: 0.5rem; padding: 1rem;
    border-top: 1px solid #222;
  }
  textarea {
    flex: 1; padding: 0.5rem 0.75rem; background: #1a1a1a;
    border: 1px solid #333; color: #eee; border-radius: 4px;
    font-family: monospace; resize: none;
  }
  textarea:disabled { opacity: 0.4; }
  button {
    padding: 0.5rem 1.2rem; background: #7cf; color: #000;
    border: none; border-radius: 4px; cursor: pointer; font-weight: bold; align-self: flex-end;
  }
  button:disabled { opacity: 0.4; cursor: default; }
  .stop-btn { background: #333; color: #f66; border: 1px solid #f66; }

  .inject-badge { margin-bottom: 0.35rem; }
  .inject-toggle {
    background: none; border: none; padding: 0; cursor: pointer;
    color: #5a8a6a; font-family: monospace; font-size: 0.75rem;
    font-weight: normal; align-self: unset;
  }
  .inject-toggle:hover { color: #7cf; }
  .inject-list {
    margin-top: 0.3rem; border-left: 2px solid #1a3a2a;
    padding-left: 0.6rem; display: flex; flex-direction: column; gap: 0.4rem;
  }
  .inject-item { display: flex; gap: 0.5rem; align-items: baseline; }
  .inject-id { color: #2a4a3a; font-size: 0.7rem; flex-shrink: 0; }
  .inject-content { color: #6a8a7a; font-size: 0.8rem; white-space: pre-wrap; word-break: break-word; }
</style>
