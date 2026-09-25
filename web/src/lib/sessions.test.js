import { describe, it, expect } from 'vitest'
import { loadSessions, saveSessions, defaultState, autoTitle, renameSession } from './sessions.js'

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

const chat = (id, name, messages = [], extra = {}) => ({ id, name, messages, ...extra })
const user = text => ({ role: 'user', content: text, text })

describe('autoTitle', () => {
  it('names an untitled session after its first message', () => {
    const [s] = autoTitle([chat(1, 'Chat 1', [user('How do I   reverse\na list?')])])
    expect(s).toMatchObject({ name: 'How do I reverse a list?', titled: true })
  })

  it('truncates long first messages', () => {
    const [s] = autoTitle([chat(1, 'Chat 1', [user('a'.repeat(60))])])
    expect(s.name).toBe('a'.repeat(31) + '…')
  })

  it('leaves titled sessions alone', () => {
    const sessions = [chat(1, 'mine', [user('hello')], { titled: true })]
    expect(autoTitle(sessions)).toBe(sessions)
  })

  it('waits for a message with text', () => {
    const sessions = [chat(1, 'Chat 1', [{ role: 'user', content: [{ type: 'image_url', image_url: { url: 'x' } }], text: '' }])]
    expect(autoTitle(sessions)).toBe(sessions)
  })
})

describe('renameSession', () => {
  it('renames and stops auto-titling', () => {
    const [s] = renameSession([chat(1, 'Chat 1')], 1, '  notes ')
    expect(s).toMatchObject({ name: 'notes', titled: true })
  })

  it('ignores a blank name', () => {
    const sessions = [chat(1, 'Chat 1')]
    expect(renameSession(sessions, 1, '   ')).toBe(sessions)
  })

  it('persists the titled flag', () => {
    const s = memStorage()
    const titledState = { sessions: [chat(1, 'notes', [], { titled: true })], activeId: 1, nextId: 2 }
    saveSessions(s, titledState)
    expect(loadSessions(s)).toEqual(titledState)
  })
})
