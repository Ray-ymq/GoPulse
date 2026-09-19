import { expect, it } from 'vitest'
import { formatDate } from '../../../frontend-shared/format'
it('uses the browser timezone across the UTC date boundary and keeps invalid input readable', () => {
  const value = '2026-09-12T23:30:00Z'
  expect(formatDate(value)).toBe(new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value)))
  expect(formatDate('invalid')).toBe('invalid')
})
