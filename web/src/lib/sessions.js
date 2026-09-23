const KEY = 'turbolab.sessions'
const TRANSIENT = ['injectedOpen', 'candidatesOpen']

export function defaultState() {
  return { sessions: [{ id: 1, name: 'Chat 1', messages: [] }], activeId: 1, nextId: 2 }
}

function validSession(s) {
  return s && Number.isInteger(s.id) && typeof s.name === 'string' && Array.isArray(s.messages)
}

export function loadSessions(storage) {
  let parsed
  try {
    parsed = JSON.parse(storage.getItem(KEY))
  } catch {
    return defaultState()
  }
  if (!parsed || !Array.isArray(parsed.sessions) || parsed.sessions.length === 0 || !parsed.sessions.every(validSession)) {
    return defaultState()
  }
  const { sessions } = parsed
  const maxId = Math.max(...sessions.map(s => s.id))
  const activeId = sessions.some(s => s.id === parsed.activeId) ? parsed.activeId : sessions[0].id
  const nextId = Number.isInteger(parsed.nextId) && parsed.nextId > maxId ? parsed.nextId : maxId + 1
  return { sessions, activeId, nextId }
}

function cleanMessage(m, dropImages) {
  const out = { ...m }
  for (const k of TRANSIENT) delete out[k]
  if (dropImages && Array.isArray(out.content)) {
    out.content = out.content.filter(p => p?.type !== 'image_url')
  }
  return out
}

function serialize({ sessions, activeId, nextId }, dropImages) {
  return JSON.stringify({
    sessions: sessions.map(s => ({ id: s.id, name: s.name, messages: s.messages.map(m => cleanMessage(m, dropImages)) })),
    activeId,
    nextId,
  })
}

export function saveSessions(storage, state) {
  for (const dropImages of [false, true]) {
    try {
      storage.setItem(KEY, serialize(state, dropImages))
      return
    } catch {}
  }
}
