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

export async function listMemories(limit = 50, offset = 0) {
  const r = await fetch(`/api/memory/list?limit=${limit}&offset=${offset}`)
  return r.json()
}

export async function searchMemories(q, limit = 20) {
  const r = await fetch(`/api/memory/search?q=${encodeURIComponent(q)}&limit=${limit}`)
  return r.json()
}

export async function addMemory(content, source = '', tags = []) {
  const r = await fetch('/api/memory/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content, source, tags }),
  })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function deleteMemory(id) {
  const r = await fetch('/api/memory/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function relateMemories(from_id, to_id, rel_type = 'related') {
  const r = await fetch('/api/memory/relate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from_id, to_id, rel_type }),
  })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function unrelateMemories(from_id, to_id, rel_type) {
  const r = await fetch('/api/memory/unrelate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from_id, to_id, rel_type }),
  })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function getMemoryGraph() {
  const r = await fetch('/api/memory/graph')
  return r.json()
}

export async function semanticSearchMemories(q, limit = 10, minScore = 0.3) {
  const r = await fetch(`/api/memory/semantic-search?q=${encodeURIComponent(q)}&limit=${limit}&min_score=${minScore}`)
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function rebuildEmbeddings() {
  const r = await fetch('/api/memory/embed-rebuild', { method: 'POST' })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function memoryStats() {
  const r = await fetch('/api/memory/stats')
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function convertFile(file) {
  const form = new FormData()
  form.append('file', file)
  const r = await fetch('/api/memory/convert', { method: 'POST', body: form })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

export async function getUsage(days = 30) {
  const r = await fetch(`/api/usage?days=${days}`)
  return r.json()
}

export async function chatStream(messages, onToken, signal, model = 'default', onCandidates = () => {}) {
  const r = await fetch('/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model, messages, stream: true }),
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
    if (data.self_consistency_candidates) onCandidates(data.self_consistency_candidates)
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
        if (chunk.self_consistency_candidates) onCandidates(chunk.self_consistency_candidates)
      } catch {}
    }
  }
}
