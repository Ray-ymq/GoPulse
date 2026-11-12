export function displayName(person: { display_name?: string; username: string }) { return person.display_name || person.username }
export function initial(name: string) { return Array.from(name)[0] || '客' }
export function timeLabel(value: string) {
  const date = new Date(value); if (Number.isNaN(date.getTime())) return ''
  const minutes = Math.floor((Date.now() - date.getTime()) / 60000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  if (minutes < 1440) return `${Math.floor(minutes / 60)} 小时前`
  return new Intl.DateTimeFormat('zh-CN', { month: 'long', day: 'numeric' }).format(date)
}
export function hashtags(text: string) { return [...new Set(text.match(/#[\p{L}\p{N}_]+/gu) || [])].slice(0, 4) }
export function validText(value: string, max: number) { const n = Array.from(value.trim()).length; return n >= 1 && n <= max }
export function safeRedirect(value: unknown) { return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') && !value.includes('\\') && !value.startsWith('/login') && !value.startsWith('/register') ? value : '/' }
