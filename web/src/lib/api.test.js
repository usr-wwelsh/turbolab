import { describe, it, expect, vi, afterEach } from 'vitest'
import { searchModels, loadModel, localModels } from './api.js'

function respond(status, body, headers = { 'content-type': 'application/json' }) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(body, { status, headers })))
}

afterEach(() => vi.unstubAllGlobals())

describe('model API errors', () => {
  it('search rejects with the server error message', async () => {
    respond(502, JSON.stringify({ error: 'huggingface unreachable' }))
    await expect(searchModels('qwen')).rejects.toThrow('huggingface unreachable')
  })

  it('load rejects with the plain-text error body', async () => {
    respond(400, 'unexpected EOF', { 'content-type': 'text/plain' })
    await expect(loadModel('org/model', 4)).rejects.toThrow('unexpected EOF')
  })

  it('local listing falls back to the HTTP status when the body is empty', async () => {
    respond(500, '')
    await expect(localModels()).rejects.toThrow('HTTP 500')
  })

  it('load resolves with the server reply on success', async () => {
    respond(202, JSON.stringify({ status: 'loading' }))
    await expect(loadModel('org/model', 4)).resolves.toEqual({ status: 'loading' })
  })
})
