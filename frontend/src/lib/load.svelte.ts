import { errorText } from './format'
import { ui } from './state.svelte'

/**
 * Loads data for a view. It runs again when a new scan or a quarantine changes the
 * data (ui.revision) and when any reactive value fn reads before its first await
 * changes. A response that arrives after a newer request started is dropped, so
 * fast clicks never show an older answer. Call it during component setup.
 */
export function loader<T>(fn: () => Promise<T>) {
  const s = $state({ data: null as T | null, error: '', loading: false })
  $effect(() => {
    void ui.revision
    let live = true
    s.error = ''
    s.loading = true
    fn()
      .then(
        (d) => {
          if (live) s.data = d
        },
        (e) => {
          if (live) s.error = errorText(e)
        },
      )
      .finally(() => {
        if (live) s.loading = false
      })
    return () => {
      live = false
    }
  })
  return s
}
