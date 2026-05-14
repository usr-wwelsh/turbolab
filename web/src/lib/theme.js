import { writable } from 'svelte/store'

const KEY = 'turbolab.theme'

export const theme = writable(load())

function load() {
  try {
    return localStorage.getItem(KEY) || 'auto'
  } catch {
    return 'auto'
  }
}

function resolve(t) {
  if (t === 'light' || t === 'dark') return t
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
  }
  return 'dark'
}

export function applyTheme(t) {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.theme = resolve(t)
}

export function setTheme(t) {
  try { localStorage.setItem(KEY, t) } catch {}
  theme.set(t)
  applyTheme(t)
}

export function initTheme() {
  applyTheme(load())
  if (typeof window !== 'undefined' && window.matchMedia) {
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    mq.addEventListener?.('change', () => {
      let cur = 'auto'
      try { cur = localStorage.getItem(KEY) || 'auto' } catch {}
      if (cur === 'auto') applyTheme('auto')
    })
  }
}
