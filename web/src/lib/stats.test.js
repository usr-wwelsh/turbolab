import { describe, it, expect } from 'vitest'
import { formatStats } from './stats.js'

describe('formatStats', () => {
  it('shows tokens, speed and context fill', () => {
    expect(formatStats({ prompt: 700, completion: 100, ms: 8000 }, 2048))
      .toEqual({ text: '100 tok · 12.5 tok/s · ctx 800/2048', warn: false })
  })

  it('warns when the context is at least 80% full', () => {
    expect(formatStats({ prompt: 1600, completion: 100, ms: 1000 }, 2048).warn).toBe(true)
  })

  it('omits context when the backend reported no prompt tokens', () => {
    expect(formatStats({ prompt: null, completion: 40, ms: 2000 }, 2048).text).toBe('40 tok · 20.0 tok/s')
  })

  it('omits context when the context size is unknown', () => {
    expect(formatStats({ prompt: 10, completion: 40, ms: 2000 }, 0).text).toBe('40 tok · 20.0 tok/s')
  })

  it('omits speed when generation took no measurable time', () => {
    expect(formatStats({ prompt: null, completion: 1, ms: 0 }, 0).text).toBe('1 tok')
  })
})
