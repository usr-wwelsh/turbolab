import { describe, it, expect } from 'vitest'
import { loadSessions, saveSessions, defaultState } from './sessions.js'

function memStorage(quota = Infinity) {
  const data = new Map()
  return {
    getItem: k => data.has(k) ? data.get(k) : null,
    setItem: (k, v) => {
      if (v.length > quota) throw new DOMException('full', 'QuotaExceededError')
      data.set(k, v)
    },
    data,
  }
}

const state = {
  sessions: [
    { id: 1, name: 'Chat 1', messages: [{ role: 'user', content: 'hi' }, { role: 'assistant', content: 'hello' }] },
    { id: 3, name: 'Chat 3', messages: [] },
  ],
  activeId: 3,
  nextId: 4,
}

describe('session persistence', () => {
  it('restores what was saved', () => {
    const s = memStorage()
    saveSessions(s, state)
    expect(loadSessions(s)).toEqual(state)
  })

  it('starts fresh when nothing is stored', () => {
    expect(loadSessions(memStorage())).toEqual(defaultState())
  })

  it('starts fresh when stored data is corrupt', () => {
    const s = memStorage()
    s.setItem('turbolab.sessions', '{not json')
    expect(loadSessions(s)).toEqual(defaultState())
  })

  it('starts fresh when stored data has the wrong shape', () => {
    const s = memStorage()
    s.setItem('turbolab.sessions', JSON.stringify({ sessions: [{ id: 'x' }], activeId: 1, nextId: 2 }))
    expect(loadSessions(s)).toEqual(defaultState())
  })

  it('falls back to the first session when the active one is missing', () => {
    const s = memStorage()
    saveSessions(s, { ...state, activeId: 99 })
    expect(loadSessions(s).activeId).toBe(1)
  })

  it('does not persist transient UI flags', () => {
    const s = memStorage()
    const msg = { role: 'assistant', content: 'x', injectedOpen: true, candidatesOpen: true }
    saveSessions(s, { sessions: [{ id: 1, name: 'Chat 1', messages: [msg] }], activeId: 1, nextId: 2 })
    const restored = loadSessions(s).sessions[0].messages[0]
    expect(restored).toEqual({ role: 'assistant', content: 'x' })
  })

  it('drops image data rather than failing when storage is full', () => {
    const img = 'data:image/png;base64,' + 'A'.repeat(5000)
    const withImage = {
      sessions: [{ id: 1, name: 'Chat 1', messages: [{ role: 'user', content: [{ type: 'text', text: 'look' }, { type: 'image_url', image_url: { url: img } }] }] }],
      activeId: 1,
      nextId: 2,
    }
    const s = memStorage(2000)
    saveSessions(s, withImage)
    const restored = loadSessions(s).sessions[0].messages[0]
    expect(restored.content).toEqual([{ type: 'text', text: 'look' }])
  })

  it('never throws when storage is unavailable', () => {
    const broken = { getItem() { throw new Error('denied') }, setItem() { throw new Error('denied') } }
    expect(() => saveSessions(broken, state)).not.toThrow()
    expect(loadSessions(broken)).toEqual(defaultState())
  })
})
