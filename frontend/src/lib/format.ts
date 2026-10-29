// Binary units (KB = 1024 bytes), matching File Explorer and the exported reports.
const units = ['KB', 'MB', 'GB', 'TB', 'PB']

export function bytes(n: number): string {
  if (!Number.isFinite(n) || n < 1024) return `${Math.max(0, Math.round(n || 0))} B`
  let v = n
  let i = -1
  do {
    v /= 1024
    i++
  } while (v >= 1024 && i < units.length - 1)
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

export function percent(p: number): string {
  if (p > 0 && p < 0.1) return '<0.1%'
  return `${p.toFixed(1)}%`
}

export function count(n: number): string {
  return n.toLocaleString()
}

export function plural(n: number, word: string): string {
  return `${count(n)} ${n === 1 ? word : word + 's'}`
}

const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' })
const dateTimeFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })

export function date(v: number | string): string {
  const d = typeof v === 'number' ? new Date(v * 1000) : new Date(v)
  if (typeof v === 'number' && v <= 0) return ''
  return Number.isNaN(d.getTime()) ? '' : dateFmt.format(d)
}

export function dateTime(v: string): string {
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? '' : dateTimeFmt.format(d)
}

export function duration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  const s = ms / 1000
  return s < 60 ? `${s.toFixed(1)} s` : `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`
}

/** Elapsed time in whole seconds, as a ticking clock shows it: "8 s", "2 min 05 s". */
export function clock(ms: number): string {
  const s = Math.floor(ms / 1000)
  return s < 60 ? `${s} s` : `${Math.floor(s / 60)} min ${String(s % 60).padStart(2, '0')} s`
}

export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  if (e && typeof e === 'object' && 'message' in e)
    return String((e as { message: unknown }).message)
  return String(e)
}

export const safetyLabel: Record<string, string> = {
  safe: 'Safe to delete',
  rebuilds: 'Rebuilds itself',
  'delete-via-app': 'Delete via the app',
  never: 'Never touch',
}
