import { Events } from '@wailsio/runtime'
import { api, type Node, type Progress, type ScanInfo, type Status } from './api'
import { listenForActivity } from './activity.svelte'
import { errorText } from './format'

export const viewIds = [
  'home',
  'explore',
  'free',
  'caches',
  'dev',
  'duplicates',
  'stale',
  'installers',
  'games',
  'cloud',
  'largest',
  'search',
  'apps',
  'hidden',
  'tools',
  'history',
  'quarantine',
  'log',
  'settings',
] as const
export type View = (typeof viewIds)[number]

export function isView(s: string): s is View {
  return (viewIds as readonly string[]).includes(s)
}

export type Theme = 'auto' | 'light' | 'dark'

/** A pending confirmation. Dialogs render from this at the app root. */
export type Dialog =
  | { kind: 'quarantine'; items: { path: string; bytes: number }[] }
  | { kind: 'action'; action: string; target: string[] }

export const ui = $state({
  status: null as Status | null,
  scan: null as ScanInfo | null,
  scanning: false,
  userScan: false, // the running scan was started here, not by the schedule
  progress: null as Progress | null,
  scanError: '',
  view: 'home' as View,
  folder: 0, // the folder open in Explore
  selected: null as Node | null,
  theme: 'auto' as Theme,
  notice: '',
  dialog: null as Dialog | null,
  searchExt: '', // set by File types to prefill Search
  // Bumped whenever scan data changes (new scan, quarantine) so views refetch.
  revision: 0,
})

let noticeTimer: ReturnType<typeof setTimeout> | undefined

export function notify(message: string) {
  ui.notice = message
  clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => (ui.notice = ''), 6000)
}

/** Runs a fire-and-forget call, reporting failure (and optionally success) in the toast. */
export function attempt(p: Promise<unknown>, success?: string) {
  p.then(
    () => success && notify(success),
    (e) => notify(errorText(e)),
  )
}

export function go(view: View) {
  ui.view = view
}

export async function refreshStatus() {
  ui.status = await api.Status()
  ui.scan = ui.status.scan
  ui.scanning = ui.status.scanning
}

export async function init() {
  applyTheme(loadTheme())
  listenForActivity()
  // A scheduled scan reports here too, so it shows in the top bar like any other.
  Events.On('scan:progress', (e) => {
    ui.scanning = true
    ui.progress = e.data
  })
  Events.On('scan:done', (e) => {
    // Only a scan the user started takes them to Explore.
    const mine = ui.userScan
    ui.scanning = ui.userScan = false
    ui.progress = null
    ui.scan = e.data
    ui.folder = e.data.rootId
    ui.selected = null
    ui.revision++
    if (mine) ui.view = 'explore'
    notify(`Scanned ${e.data.root}.`)
  })
  Events.On('scan:error', (e) => {
    ui.scanning = ui.userScan = false
    ui.progress = null
    ui.scanError = e.data
  })
  try {
    await refreshStatus()
  } catch (e) {
    ui.scanError = errorText(e)
  }
  if (ui.scan) {
    ui.folder = ui.scan.rootId
    ui.view = 'explore'
  }
}

export async function startScan(root: string) {
  ui.scanError = ''
  // Set before the call returns: a small folder can finish, and send scan:done, first.
  ui.scanning = ui.userScan = true
  ui.progress = null
  try {
    await api.StartScan(root)
  } catch (e) {
    ui.scanning = ui.userScan = false
    ui.scanError = errorText(e)
  }
}

export async function reveal(path: string) {
  try {
    const n = await api.FindPath(path)
    const chain = (await api.NodeWithPath(n.id)) ?? []
    const parent = chain.length > 1 ? chain[chain.length - 2] : n
    ui.folder = n.isDir && chain.length === 1 ? n.id : parent.id
    ui.selected = n
    ui.view = 'explore'
  } catch (e) {
    notify(errorText(e))
  }
}

export function askQuarantine(items: { path: string; bytes: number }[]) {
  if (items.length) ui.dialog = { kind: 'quarantine', items }
}

export function askAction(action: string, target: string[] = []) {
  ui.dialog = { kind: 'action', action, target }
}

const themeKey = 'unhoard.theme'

function loadTheme(): Theme {
  try {
    const t = localStorage.getItem(themeKey)
    if (t === 'light' || t === 'dark') return t
  } catch {
    // storage unavailable: follow Windows
  }
  return 'auto'
}

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)')
darkQuery.addEventListener('change', () => applyTheme(ui.theme))

export function applyTheme(t: Theme) {
  ui.theme = t
  try {
    localStorage.setItem(themeKey, t)
  } catch {
    // not fatal; the choice lasts for this session
  }
  const dark = t === 'dark' || (t === 'auto' && darkQuery.matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}
