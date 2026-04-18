export async function getStatus() {
  const r = await fetch('/api/status')
  return r.json()
}

export async function loadModel(model, bits) {
  const r = await fetch('/api/load', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model, bits }),
  })
  return r.json()
}

export async function searchModels(query, limit = 20) {
  const r = await fetch(`/api/models/search?q=${encodeURIComponent(query)}&limit=${limit}`)
  return r.json()
}

export async function localModels() {
  const r = await fetch('/api/models/local')
  return r.json()
}

export async function deleteModel(model) {
  const r = await fetch('/api/models/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model }),
  })
  if (!r.ok) {
    const data = await r.json().catch(() => ({}))
    throw new Error(data.error || `HTTP ${r.status}`)
  }
  return r.json()
}

export async function getConfig() {
  const r = await fetch('/api/config')
  return r.json()
}

export async function saveConfig(cfg) {
  const r = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  })
  return r.json()
}

export async function getUsage(days = 30) {
  const r = await fetch(`/api/usage?days=${days}`)
  return r.json()
}

export async function chatStream(messages, onToken, signal) {
  const r = await fetch('/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: 'default', messages, stream: true }),
    signal,
  })
  if (!r.ok) {
    const data = await r.json().catch(() => ({}))
    const err = new Error(data.error ?? `HTTP ${r.status}`)
    err.detail = JSON.stringify(data, null, 2)
    throw err
  }
  const contentType = r.headers.get('content-type') ?? ''
  if (!contentType.includes('text/event-stream')) {
    const data = await r.json()
    const content = data.choices?.[0]?.message?.content
    if (content) onToken(content)
    return
  }
  const reader = r.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    const lines = buf.split('\n')
    buf = lines.pop()
    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      const payload = line.slice(6).trim()
      if (payload === '[DONE]') return
      try {
        const chunk = JSON.parse(payload)
        const token = chunk.choices?.[0]?.delta?.content
        if (token) onToken(token)
      } catch {}
    }
  }
}
