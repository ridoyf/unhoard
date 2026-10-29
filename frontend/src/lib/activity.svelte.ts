import { Events } from '@wailsio/runtime'

/** A job that runs longer than a click: shown in the top bar until it ends. */
export type Activity = {
  id: string
  label: string
  percent: number // 0 to 100, or -1 when the job cannot tell
  detail: string
  startedAt: number
  changedAt: number // when percent or detail last changed, for the "no change" warning
}

export const activities = $state<Record<string, Activity>>({})

export function startActivity(id: string, label: string) {
  const now = Date.now()
  activities[id] = { id, label, percent: -1, detail: '', startedAt: now, changedAt: now }
}

export function endActivity(id: string) {
  delete activities[id]
}

/** Runs work as activity id, so it shows as running until it settles either way. */
export async function tracked<T>(id: string, label: string, work: () => Promise<T>): Promise<T> {
  startActivity(id, label)
  try {
    return await work()
  } finally {
    endActivity(id)
  }
}

export function listenForActivity() {
  Events.On('activity:progress', (e) => {
    const a = activities[e.data.id]
    if (!a) return
    if (e.data.percent !== a.percent || e.data.detail !== a.detail) a.changedAt = Date.now()
    a.percent = e.data.percent
    a.detail = e.data.detail
  })
  // The duplicate search is started by one call and finished by this event.
  Events.On('dups:done', () => endActivity('duplicates'))
}
