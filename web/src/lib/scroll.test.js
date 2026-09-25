import { describe, it, expect } from 'vitest'
import { nearBottom } from './scroll.js'

const el = (scrollTop, scrollHeight = 1000, clientHeight = 400) => ({ scrollTop, scrollHeight, clientHeight })

describe('nearBottom', () => {
  it('is true when scrolled to the end', () => {
    expect(nearBottom(el(600))).toBe(true)
  })

  it('tolerates being a few lines short of the end', () => {
    expect(nearBottom(el(560))).toBe(true)
  })

  it('is false once the reader has scrolled up', () => {
    expect(nearBottom(el(300))).toBe(false)
  })

  it('is true when content is shorter than the viewport', () => {
    expect(nearBottom(el(0, 200))).toBe(true)
  })
})
