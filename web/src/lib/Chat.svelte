<script>
  import { chatStream, semanticSearchMemories, convertFile } from './api.js'

  export let modelRunning = false
  export let model = ''
  export let memInject = false
  export let messages = []
  export const sessionId = 0
  export let onStreaming = (_) => {}

  let input = ''
  let attachments = [] // {kind:'image'|'text', name, dataURL?, text?}
  let streaming = false
  let gotTokens = false
  let abortCtrl = null
  let chatEl
  let historyIdx = -1
  let savedDraft = ''
  let textareaEl
  let fileInputEl
  let draggingOver = false
  let attachError = null

  $: userHistory = messages
    .filter(m => m.role === 'user')
    .map(m => msgText(m.content))

  function msgText(content) {
    if (typeof content === 'string') return content
    if (Array.isArray(content)) {
      return content.filter(p => p?.type === 'text').map(p => p.text).join('\n')
    }
    return ''
  }

  function msgImages(content) {
    if (!Array.isArray(content)) return []
    return content.filter(p => p?.type === 'image_url').map(p => p.image_url?.url).filter(Boolean)
  }

  function buildContent(text, atts) {
    const hasImages = atts.some(a => a.kind === 'image')
    let fullText = text
    const textBlobs = atts.filter(a => a.kind === 'text')
    if (textBlobs.length > 0) {
      const blobs = textBlobs.map(a => `# ${a.name}\n\n${a.text}`).join('\n\n---\n\n')
      fullText = fullText ? `${fullText}\n\n---\n\n${blobs}` : blobs
    }
    if (!hasImages) return fullText
    const parts = []
    if (fullText) parts.push({ type: 'text', text: fullText })
    for (const a of atts) {
      if (a.kind === 'image') {
        parts.push({ type: 'image_url', image_url: { url: a.dataURL } })
      }
    }
    return parts
  }

  async function send() {
    if ((!input.trim() && attachments.length === 0) || streaming) return
    const content = buildContent(input, attachments)
    const userMsg = { role: 'user', content }
    messages = [...messages, userMsg]
    input = ''
    attachments = []
    historyIdx = -1
    savedDraft = ''
    streaming = true
    gotTokens = false
    onStreaming(true)
    abortCtrl = new AbortController()

    const assistantMsg = { role: 'assistant', content: '', injected: null, injectedOpen: false }
    messages = [...messages, assistantMsg]
    scrollBottom()

    if (memInject) {
      const shownIds = new Set(messages.flatMap(m => m.injected?.map(i => i.id) ?? []))
      const searchText = msgText(userMsg.content)
      semanticSearchMemories(searchText, 2, 0.6).then(mems => {
        const fresh = (mems ?? []).filter(m => !shownIds.has(m.id))
        if (fresh.length > 0) {
          assistantMsg.injected = fresh
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

  function readImage(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result)
      reader.onerror = reject
      reader.readAsDataURL(file)
    })
  }

  async function addFile(file) {
    if (!file) return
    attachError = null
    try {
      if (file.type.startsWith('image/')) {
        const dataURL = await readImage(file)
        attachments = [...attachments, { kind: 'image', name: file.name, dataURL }]
      } else {
        const result = await convertFile(file)
        attachments = [...attachments, { kind: 'text', name: result.filename || file.name, text: result.markdown }]
      }
    } catch (e) {
      attachError = `${file.name}: ${e.message}`
    }
  }

  async function onFileInput(e) {
    for (const f of e.target.files) await addFile(f)
    e.target.value = ''
  }

  function onDragOver(e) {
    if (!modelRunning) return
    e.preventDefault()
    draggingOver = true
  }
  function onDragLeave() { draggingOver = false }
  async function onDrop(e) {
    e.preventDefault()
    draggingOver = false
    if (!modelRunning) return
    for (const f of e.dataTransfer.files) await addFile(f)
  }

  async function onPaste(e) {
    const items = e.clipboardData?.items
    if (!items) return
    for (const it of items) {
      if (it.kind === 'file') {
        const f = it.getAsFile()
        if (f) {
          e.preventDefault()
          await addFile(f)
        }
      }
    }
  }

  function removeAttachment(i) {
    attachments = attachments.filter((_, idx) => idx !== i)
  }
</script>

<div
  class="chat"
  class:dragging={draggingOver}
  on:dragover={onDragOver}
  on:dragleave={onDragLeave}
  on:drop={onDrop}
  role="region"
>
  <div class="messages" bind:this={chatEl}>
    {#if messages.length === 0}
      <div class="empty">
        {modelRunning ? 'Start chatting. Drop files or paste images.' : 'Load a model first.'}
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
          {#if msgImages(msg.content).length > 0}
            <div class="img-row">
              {#each msgImages(msg.content) as url}
                <img src={url} alt="" class="att-img" />
              {/each}
            </div>
          {/if}
          <pre class="content" class:error={msg.error}>{msgText(msg.content)}{#if msg.role === 'assistant' && streaming && msg === messages[messages.length - 1]}<span class="cursor">▋</span>{/if}</pre>
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
  {#if attachments.length > 0 || attachError}
    <div class="att-bar">
      {#each attachments as a, i}
        <div class="att-chip" title={a.name}>
          {#if a.kind === 'image'}
            <img src={a.dataURL} alt="" class="att-thumb" />
          {:else}
            <span class="att-icon">📄</span>
          {/if}
          <span class="att-name">{a.name}</span>
          <button class="att-x" on:click={() => removeAttachment(i)} title="remove">×</button>
        </div>
      {/each}
      {#if attachError}<span class="att-err">{attachError}</span>{/if}
    </div>
  {/if}
  <div class="input-row">
    <button
      class="attach-btn"
      on:click={() => fileInputEl?.click()}
      disabled={!modelRunning || streaming}
      title="Attach file or image"
    >📎</button>
    <input type="file" multiple bind:this={fileInputEl} on:change={onFileInput} style="display:none" />
    <textarea
      bind:this={textareaEl}
      bind:value={input}
      on:keydown={onKey}
      on:paste={onPaste}
      placeholder={modelRunning ? 'Message... (↑↓ history, drop/paste files)' : 'Load a model to chat'}
      disabled={!modelRunning || streaming}
      rows="2"
    ></textarea>
    {#if streaming}
      <button class="stop-btn" on:click={stop}>Stop</button>
    {:else}
      <button on:click={send} disabled={!modelRunning}>Send</button>
    {/if}
  </div>
  {#if draggingOver}
    <div class="drop-overlay">drop to attach</div>
  {/if}
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; position: relative; }
  .chat.dragging { outline: 2px dashed var(--accent); outline-offset: -8px; }
  .messages {
    flex: 1; overflow-y: auto; padding: 1rem;
    display: flex; flex-direction: column; gap: 1rem;
  }
  .empty { color: var(--fg-6); font-family: monospace; text-align: center; margin-top: 4rem; }
  .msg { display: flex; gap: 0.75rem; }
  .role { color: var(--fg-3); font-family: monospace; font-size: 0.8rem; min-width: 2rem; padding-top: 0.1rem; }
  .msg.user .role { color: var(--accent); }
  .msg.assistant .role { color: var(--success); }
  .content-wrap { flex: 1; min-width: 0; }
  .content {
    margin: 0; font-family: monospace; font-size: 0.9rem;
    color: var(--fg-1); white-space: pre-wrap; word-break: break-word;
  }
  .content.error { color: var(--error); }
  .cursor { animation: blink 1s step-end infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .error-detail { margin-top: 0.4rem; }
  .error-detail summary { color: var(--fg-5); font-size: 0.8rem; cursor: pointer; }
  .error-detail pre { color: var(--fg-3); font-size: 0.78rem; margin: 0.25rem 0 0; white-space: pre-wrap; }
  .generating {
    padding: 0.25rem 1rem; font-size: 0.75rem; color: var(--purple);
    border-top: 1px solid var(--border-faint); font-family: monospace;
  }
  .dots span { animation: appear 1.2s infinite; opacity: 0; }
  .dots span:nth-child(2) { animation-delay: 0.4s; }
  .dots span:nth-child(3) { animation-delay: 0.8s; }
  @keyframes appear { 0%, 100% { opacity: 0; } 50% { opacity: 1; } }

  .img-row { display: flex; gap: 0.4rem; flex-wrap: wrap; margin-bottom: 0.5rem; }
  .att-img {
    max-width: 240px; max-height: 240px; border-radius: 4px;
    border: 1px solid var(--border-subtle);
  }

  .att-bar {
    display: flex; gap: 0.4rem; flex-wrap: wrap; align-items: center;
    padding: 0.5rem 1rem; border-top: 1px solid var(--border-faint);
  }
  .att-chip {
    display: flex; align-items: center; gap: 0.4rem;
    padding: 0.2rem 0.5rem; background: var(--bg-card);
    border: 1px solid var(--border-soft); border-radius: 4px;
    font-size: 0.75rem; color: var(--fg-1);
  }
  .att-thumb { width: 24px; height: 24px; object-fit: cover; border-radius: 2px; }
  .att-icon { font-size: 0.85rem; }
  .att-name { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .att-x {
    background: none; border: none; color: var(--fg-5);
    cursor: pointer; font-size: 1rem; line-height: 1; padding: 0 0.1rem;
    font-family: monospace;
  }
  .att-x:hover { color: var(--error); }
  .att-err { color: var(--error); font-size: 0.75rem; }

  .input-row {
    display: flex; gap: 0.5rem; padding: 1rem;
    border-top: 1px solid var(--border-subtle);
  }
  .attach-btn {
    padding: 0 0.6rem; background: var(--bg-card); border: 1px solid var(--border);
    color: var(--fg-2); border-radius: 4px; cursor: pointer;
    font-family: monospace; font-size: 1rem; align-self: stretch;
  }
  .attach-btn:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
  .attach-btn:disabled { opacity: 0.4; cursor: default; }
  textarea {
    flex: 1; padding: 0.5rem 0.75rem; background: var(--bg-card);
    border: 1px solid var(--border); color: var(--fg); border-radius: 4px;
    font-family: monospace; resize: none;
  }
  textarea:disabled { opacity: 0.4; }
  button {
    padding: 0.5rem 1.2rem; background: var(--accent); color: var(--accent-fg);
    border: none; border-radius: 4px; cursor: pointer; font-weight: bold; align-self: flex-end;
  }
  button:disabled { opacity: 0.4; cursor: default; }
  .stop-btn { background: var(--bg-card); color: var(--error); border: 1px solid var(--error); }

  .drop-overlay {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    color: var(--accent); font-size: 1.2rem; pointer-events: none;
    font-family: monospace;
  }

  .inject-badge { margin-bottom: 0.35rem; }
  .inject-toggle {
    background: none; border: none; padding: 0; cursor: pointer;
    color: var(--inject-fg); font-family: monospace; font-size: 0.75rem;
    font-weight: normal; align-self: unset;
  }
  .inject-toggle:hover { color: var(--accent); }
  .inject-list {
    margin-top: 0.3rem; border-left: 2px solid var(--inject-border);
    padding-left: 0.6rem; display: flex; flex-direction: column; gap: 0.4rem;
  }
  .inject-item { display: flex; gap: 0.5rem; align-items: baseline; }
  .inject-id { color: var(--inject-id-fg); font-size: 0.7rem; flex-shrink: 0; }
  .inject-content { color: var(--inject-fg-dim); font-size: 0.8rem; white-space: pre-wrap; word-break: break-word; }
</style>
