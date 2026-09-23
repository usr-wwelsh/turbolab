export function msgText(content) {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content.filter(p => p?.type === 'text').map(p => p.text).join('\n')
  }
  return ''
}

export function msgImages(content) {
  if (!Array.isArray(content)) return []
  return content.filter(p => p?.type === 'image_url').map(p => p.image_url?.url).filter(Boolean)
}

export function regenerateHistory(messages) {
  let last = messages.length - 1
  while (last >= 0 && messages[last].role !== 'user') last--
  return messages.slice(0, last + 1)
}

export function editHistory(messages, idx) {
  const msg = messages[idx]
  const attachments = [
    ...(msg.files ?? []).map(f => ({ kind: 'text', name: f.name, text: f.text })),
    ...msgImages(msg.content).map(url => ({ kind: 'image', name: 'image', dataURL: url })),
  ]
  return {
    history: messages.slice(0, idx),
    draft: { text: msg.text ?? msgText(msg.content), attachments },
  }
}
