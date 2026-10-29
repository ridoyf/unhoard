import { api, type Fix } from './api'
import { askAction, attempt, go, isView, startScan, ui, type View } from './state.svelte'

/** Cleanup actions whose target is chosen on another page rather than in the preview dialog. */
export const pickerView: Record<string, { view: View; label: string }> = {
  'uninstall-app': { view: 'apps', label: 'Open Apps' },
  'cloud-online-only': { view: 'cloud', label: 'Open Cloud copies' },
}

/** What a long silence usually means while each action runs, shown when progress stops moving. */
export const slowHint: Record<string, string> = {
  'dism-component-cleanup':
    'DISM often stays at the same percentage for several minutes, especially around 10% and 20%. It is still working.',
  'wsl-compact':
    'Compacting a large virtual disk can stay at one percentage for a long time while diskpart works through it.',
  'empty-recycle-bin':
    'A large folder only counts as removed once all of it is gone, so the count can pause while Windows works through it.',
  'docker-prune': 'Docker prints nothing until it has finished removing each kind of data.',
}

/** Whether an action's target is a list of paths, so it can run on one known item. */
export function takesPath(action: string): boolean {
  return action === 'cloud-online-only'
}

export function openAction(action: string, path?: string) {
  if (path && takesPath(action)) return askAction(action, [path])
  const picker = pickerView[action]
  if (picker) return go(picker.view)
  askAction(action)
}

/** Carries out a fix the backend suggested. Fixes without a kind are advice and do nothing here. */
export function applyFix(fix: Fix, context: { path?: string; retry?: () => void }) {
  switch (fix.kind) {
    case 'retry':
      context.retry?.()
      break
    case 'admin':
      attempt(api.RelaunchAsAdmin())
      break
    case 'rescan':
      if (ui.scan) startScan(ui.scan.root)
      break
    case 'action':
      openAction(fix.arg, context.path)
      break
    case 'view':
      if (isView(fix.arg)) go(fix.arg)
      break
    case 'explorer':
      attempt(api.OpenInExplorer(fix.arg))
      break
  }
}
