import { describe, it, expect } from 'vitest'
import { regenerateHistory, editHistory } from './chatActions.js'

const u = (text, extra = {}) => ({ role: 'user', content: text, text, ...extra })
const a = content => ({ role: 'assistant', content })

describe('regenerateHistory', () => {
  it('drops the reply after the last user message', () => {
    expect(regenerateHistory([u('q1'), a('r1'), u('q2'), a('r2')])).toEqual([u('q1'), a('r1'), u('q2')])
  })

  it('keeps history intact when the last message is already from the user', () => {
    expect(regenerateHistory([u('q1'), a('r1'), u('q2')])).toEqual([u('q1'), a('r1'), u('q2')])
  })

  it('returns nothing when there is no user message to answer', () => {
    expect(regenerateHistory([a('hi')])).toEqual([])
  })
})

describe('editHistory', () => {
  it('cuts history before the edited message and returns its text as the draft', () => {
    const { history, draft } = editHistory([u('q1'), a('r1'), u('q2'), a('r2')], 2)
    expect(history).toEqual([u('q1'), a('r1')])
    expect(draft.text).toBe('q2')
    expect(draft.attachments).toEqual([])
  })

  it('restores attached documents and images as attachments', () => {
    const msg = u('look', {
      content: [{ type: 'text', text: 'look' }, { type: 'image_url', image_url: { url: 'data:image/png;base64,AA' } }],
      files: [{ name: 'notes.md', text: '# notes' }],
    })
    const { draft } = editHistory([msg], 0)
    expect(draft.text).toBe('look')
    expect(draft.attachments).toEqual([
      { kind: 'text', name: 'notes.md', text: '# notes' },
      { kind: 'image', name: 'image', dataURL: 'data:image/png;base64,AA' },
    ])
  })

  it('falls back to message content for messages saved without raw text', () => {
    const { draft } = editHistory([{ role: 'user', content: 'old' }], 0)
    expect(draft.text).toBe('old')
  })
})
