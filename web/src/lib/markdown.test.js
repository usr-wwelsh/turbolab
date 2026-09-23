import { describe, it, expect } from 'vitest'
import { renderMarkdown } from './markdown.js'

const ALLOWED_TAG = /<\/?(p|br|strong|em|code|pre|h[1-6]|ul|ol|li|blockquote|hr|a|div|button)(\s[^<>]*)?>/g

describe('renderMarkdown', () => {
  it('wraps plain text in a paragraph', () => {
    expect(renderMarkdown('hello')).toBe('<p>hello</p>')
  })

  it('separates paragraphs on blank lines and keeps single newlines as breaks', () => {
    expect(renderMarkdown('a\nb\n\nc')).toBe('<p>a<br>b</p><p>c</p>')
  })

  it('renders bold, italic and inline code', () => {
    expect(renderMarkdown('**b** *i* `c`')).toBe('<p><strong>b</strong> <em>i</em> <code>c</code></p>')
  })

  it('does not format inside inline code', () => {
    expect(renderMarkdown('`**x**`')).toBe('<p><code>**x**</code></p>')
  })

  it('renders headings', () => {
    expect(renderMarkdown('## Title')).toBe('<h2>Title</h2>')
  })

  it('renders unordered and ordered lists', () => {
    expect(renderMarkdown('- a\n- b')).toBe('<ul><li>a</li><li>b</li></ul>')
    expect(renderMarkdown('1. a\n2. b')).toBe('<ol><li>a</li><li>b</li></ol>')
  })

  it('renders blockquotes and rules', () => {
    expect(renderMarkdown('> q')).toBe('<blockquote>q</blockquote>')
    expect(renderMarkdown('---')).toBe('<hr>')
  })

  it('renders fenced code blocks verbatim with a copy button', () => {
    const out = renderMarkdown('```js\nconst a = 1 * 2\n```')
    expect(out).toContain('<pre><code>const a = 1 * 2</code></pre>')
    expect(out).toContain('<button type="button" class="code-copy">copy</button>')
  })

  it('treats an unclosed fence as code to the end, so streaming renders sanely', () => {
    expect(renderMarkdown('```\nline 1\nline 2')).toContain('<pre><code>line 1\nline 2</code></pre>')
  })

  it('renders http links that open safely in a new tab', () => {
    expect(renderMarkdown('[site](https://wwel.sh)')).toBe(
      '<p><a href="https://wwel.sh" target="_blank" rel="noopener noreferrer">site</a></p>'
    )
  })

  it('leaves non-http links as plain text', () => {
    expect(renderMarkdown('[x](javascript:alert(1))')).not.toContain('<a')
  })

  it('escapes raw HTML', () => {
    expect(renderMarkdown('<img src=x onerror=alert(1)>')).toBe('<p>&lt;img src=x onerror=alert(1)&gt;</p>')
  })

  it('cannot break out of a link href with quotes', () => {
    const out = renderMarkdown('[x](https://a.b/"onmouseover="alert(1))')
    expect(out).not.toMatch(/"\s*onmouseover=/)
  })

  it('never emits a tag outside its allowlist for arbitrary input', () => {
    const pieces = ['<', '>', '"', "'", '&', '`', '```', '*', '**', '[', ']', '(', ')', 'https://x', 'javascript:', '\n', '\n\n', '- ', '# ', '> ', 'script', 'on', '=', ' ']
    let seed = 42
    const rand = n => { seed = (seed * 1103515245 + 12345) & 0x7fffffff; return seed % n }
    for (let i = 0; i < 2000; i++) {
      let s = ''
      const len = rand(30)
      for (let j = 0; j < len; j++) s += pieces[rand(pieces.length)]
      const out = renderMarkdown(s)
      expect(out.replace(ALLOWED_TAG, ''), JSON.stringify(s)).not.toMatch(/[<>]/)
      expect(out, JSON.stringify(s)).not.toMatch(/href="(?!https?:\/\/)/)
    }
  })
})
