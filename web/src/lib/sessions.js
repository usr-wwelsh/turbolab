import { msgText } from './chatActions.js'

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
    sessions: sessions.map(s => ({ id: s.id, name: s.name, ...(s.titled && { titled: true }), messages: s.messages.map(m => cleanMessage(m, dropImages)) })),
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

const TITLE_MAX = 32

function titleFrom(msg) {
  const text = (msg.text ?? msgText(msg.content)).replace(/\s+/g, ' ').trim()
  return text.length > TITLE_MAX ? text.slice(0, TITLE_MAX - 1) + '…' : text
}

export function autoTitle(sessions) {
  let changed = false
  const out = sessions.map(s => {
    if (s.titled) return s
    const first = s.messages.find(m => m.role === 'user')
    const name = first && titleFrom(first)
    if (!name) return s
    changed = true
    return { ...s, name, titled: true }
  })
  return changed ? out : sessions
}

export function renameSession(sessions, id, name) {
  const trimmed = name.trim()
  if (!trimmed) return sessions
  return sessions.map(s => s.id === id ? { ...s, name: trimmed, titled: true } : s)
}
