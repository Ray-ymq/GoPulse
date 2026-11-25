import { expect, it } from 'vitest'
import { hashtags, safeRedirect, validText } from '../src/utils/format'
it('counts Unicode code points and rejects whitespace-only inputs', () => { expect(validText('😀', 1)).toBe(true); expect(validText('  ', 10)).toBe(false); expect(validText('😀😀', 1)).toBe(false) })
it('allows only local redirect destinations', () => { for (const value of ['https://example.com', '//example.com', '/\\example.com', '/login', ['/me']]) expect(safeRedirect(value)).toBe('/'); expect(safeRedirect('/posts/3#comments')).toBe('/posts/3#comments') })
it('extracts unique Unicode hashtags without interpreting HTML', () => { expect(hashtags('#生活 #生活 #设计 <script>')).toEqual(['#生活', '#设计']) })
