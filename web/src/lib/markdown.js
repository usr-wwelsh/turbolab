const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }

function esc(s) {
  return s.replace(/[&<>"']/g, c => ESC[c])
}

function formatText(escaped) {
  return escaped
    .replace(/\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*([^*\n]+)\*/g, '<em>$1</em>')
}

function inline(text) {
  return text
    .split(/`([^`\n]+)`/)
    .map((seg, i) => i % 2 ? `<code>${esc(seg)}</code>` : formatText(esc(seg)))
    .join('')
}

const FENCE = /^\s*```/
const HEADING = /^(#{1,6})\s+(.*)$/
const RULE = /^\s*(-{3,}|\*{3,}|_{3,})\s*$/
const UL = /^\s*[-*+]\s+(.*)$/
const OL = /^\s*\d+[.)]\s+(.*)$/
const QUOTE = /^>\s?(.*)$/

function codeBlock(code) {
  return `<div class="codeblock"><button type="button" class="code-copy">copy</button><pre><code>${esc(code)}</code></pre></div>`
}

export function renderMarkdown(src) {
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  const out = []
  let para = []
  const flush = () => {
    if (para.length) out.push(`<p>${para.map(inline).join('<br>')}</p>`)
    para = []
  }
  const collect = (i, re) => {
    const items = []
    while (i < lines.length && re.test(lines[i])) items.push(lines[i++].match(re)[1])
    return [items, i]
  }

  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    let m
    if (FENCE.test(line)) {
      flush()
      const code = []
      i++
      while (i < lines.length && !FENCE.test(lines[i])) code.push(lines[i++])
      i++
      out.push(codeBlock(code.join('\n')))
    } else if (line.trim() === '') {
      flush()
      i++
    } else if ((m = line.match(HEADING))) {
      flush()
      out.push(`<h${m[1].length}>${inline(m[2])}</h${m[1].length}>`)
      i++
    } else if (RULE.test(line)) {
      flush()
      out.push('<hr>')
      i++
    } else if (UL.test(line) || OL.test(line)) {
      flush()
      const [re, tag] = UL.test(line) ? [UL, 'ul'] : [OL, 'ol']
      let items
      ;[items, i] = collect(i, re)
      out.push(`<${tag}>${items.map(t => `<li>${inline(t)}</li>`).join('')}</${tag}>`)
    } else if (QUOTE.test(line)) {
      flush()
      let items
      ;[items, i] = collect(i, QUOTE)
      out.push(`<blockquote>${items.map(inline).join('<br>')}</blockquote>`)
    } else {
      para.push(line)
      i++
    }
  }
  flush()
  return out.join('')
}
