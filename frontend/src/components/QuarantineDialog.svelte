<script lang="ts">
  import { api, type MoveResult } from '../lib/api'
  import { activities, tracked } from '../lib/activity.svelte'
  import { bytes, errorText, plural } from '../lib/format'
  import { notify, ui } from '../lib/state.svelte'
  import Busy from './Busy.svelte'
  import Modal from './Modal.svelte'
  import ProblemView from './ProblemView.svelte'

  let { items, onclose }: { items: { path: string; bytes: number }[]; onclose: () => void } =
    $props()

  const total = $derived(items.reduce((s, i) => s + i.bytes, 0))
  let busy = $state(false)
  let results = $state<MoveResult[] | null>(null)
  let days = $state(7)

  $effect(() => {
    api.Settings().then((s) => (days = s.retentionDays))
  })

  async function quarantine(paths: string[]): Promise<MoveResult[]> {
    const label = `Moving ${plural(paths.length, 'item')} to quarantine`
    try {
      return (await tracked('quarantine', label, () => api.Quarantine(paths))) ?? []
    } finally {
      ui.revision++
    }
  }

  async function move() {
    busy = true
    try {
      results = await quarantine(items.map((i) => i.path))
      if (results.every((r) => r.ok)) {
        notify(`Moved ${plural(results.length, 'item')} to quarantine.`)
        onclose()
      }
    } catch (e) {
      notify(errorText(e))
    } finally {
      busy = false
    }
  }

  async function retry(path: string) {
    busy = true
    try {
      const [again] = await quarantine([path])
      results = (results ?? []).map((r) => (r.path === path ? again : r))
    } catch (e) {
      notify(errorText(e))
    } finally {
      busy = false
    }
  }

  const moved = $derived(results?.filter((r) => r.ok).length ?? 0)
  const failed = $derived(results?.filter((r) => !r.ok) ?? [])
  const summary = $derived.by(() => {
    const stayed =
      failed.length === 1 ? 'This stayed where it was:' : 'These stayed where they were:'
    if (!failed.length) return `${plural(moved, 'item')} moved.`
    return moved ? `${plural(moved, 'item')} moved. ${stayed}` : `Nothing was moved. ${stayed}`
  })
  const title = $derived(
    results
      ? failed.length
        ? 'Some items were not moved'
        : 'Everything was moved'
      : `Move ${items.length === 1 ? 'this item' : plural(items.length, 'item')} to quarantine?`,
  )
</script>

<Modal {title} {busy} {onclose}>
  {#if busy}
    <Busy activity={activities.quarantine} label="Moving to quarantine" />
  {:else if results}
    <p>{summary}</p>
    {#each failed as r (r.path)}
      <div class="failed">
        <p class="path">{r.path}</p>
        {#if r.problem}
          <ProblemView problem={r.problem} path={r.path} onretry={() => retry(r.path)} />
        {/if}
      </div>
    {/each}
  {:else}
    <ul class="list">
      {#each items as i}
        <li>
          <span class="path">{i.path}</span>
          <span class="size">{bytes(i.bytes)}</span>
        </li>
      {/each}
    </ul>
    <p>
      Quarantined items are moved aside, not deleted. You can restore them from Quarantine for {days}
      days, after which they are deleted for good. The {bytes(total)} is freed once they are deleted,
      or straight away if you empty the quarantine.
    </p>
  {/if}
  {#snippet footer()}
    {#if results}
      <button class="primary" onclick={onclose} disabled={busy}>Close</button>
    {:else}
      <button onclick={onclose} disabled={busy}>Cancel</button>
      <button class="primary" disabled={busy} onclick={move}>Move to quarantine</button>
    {/if}
  {/snippet}
</Modal>

<style>
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 260px;
    overflow: auto;
    border: 1px solid var(--line);
    border-radius: var(--radius-s);
  }
  li {
    display: flex;
    gap: 12px;
    justify-content: space-between;
    padding: 6px 10px;
    border-bottom: 1px solid var(--line);
  }
  li:last-child {
    border-bottom: none;
  }
  .path {
    word-break: break-all;
  }
  .size {
    white-space: nowrap;
  }
  .failed {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .failed .path {
    font-weight: 600;
  }
</style>
