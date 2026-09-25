export function formatStats({ prompt, completion, ms }, ctxSize) {
  const parts = [`${completion} tok`]
  if (ms > 0) parts.push(`${(completion / (ms / 1000)).toFixed(1)} tok/s`)
  let warn = false
  if (prompt != null && ctxSize > 0) {
    const used = prompt + completion
    parts.push(`ctx ${used}/${ctxSize}`)
    warn = used / ctxSize >= 0.8
  }
  return { text: parts.join(' · '), warn }
}
