export function nearBottom({ scrollTop, scrollHeight, clientHeight }, slack = 48) {
  return scrollHeight - scrollTop - clientHeight <= slack
}
