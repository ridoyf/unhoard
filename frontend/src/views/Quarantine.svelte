<script lang="ts">
  import { api, type Problem, type QuarantineItem } from '../lib/api'
  import { activities, tracked } from '../lib/activity.svelte'
  import { bytes, dateTime, errorText, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { notify, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import Modal from '../components/Modal.svelte'
  import ProblemView from '../components/ProblemView.svelte'

  const res = loader(async () => {
    const [items, settings] = await Promise.all([api.QuarantineItems(), api.Settings()])
    return { items: items ?? [], days: settings.retentionDays }
  })
  const items = $derived(res.data?.items ?? [])
  const days = $derived(res.data?.days ?? 7)
  let confirm = $state<{ title: string; text: string; go: () => void } | null>(null)
  let busy = $state(false)
  let failure = $state<{ problem: Problem; path: string; retry: () => void } | null>(null)

  const total = $derived(items.reduce((s, i) => s + i.bytes, 0))

  function emptyAll() {
    confirm = {
      title: 'Delete everything in quarantine?',
      text: `This permanently deletes ${plural(items.length, 'item')} (${bytes(total)}).`,
      go: () =>
        run('Emptying the quarantine', 'Quarantine emptied.', '', () => api.EmptyQuarantine()),
    }
  }

  function restore(it: QuarantineItem) {
    run('Restoring ' + name(it), 'Restored.', it.originalPath, () => api.Restore(it.id))
  }

  function remove(it: QuarantineItem) {
    confirm = {
      title: `Delete ${name(it)} for good?`,
      text: `This permanently deletes ${it.originalPath} (${bytes(it.bytes)}).`,
      go: () =>
        run('Deleting ' + name(it), 'Deleted for good.', it.originalPath, () =>
          api.DeleteQuarantined(it.id),
        ),
    }
  }

  function confirmed() {
    confirm?.go()
    confirm = null
  }

  function name(it: QuarantineItem): string {
    return it.originalPath.split('\\').pop() ?? it.originalPath
  }

  function expires(it: QuarantineItem): string {
    const d = new Date(it.quarantinedAt)
    d.setDate(d.getDate() + days)
    return dateTime(d.toISOString())
  }

  async function run(label: string, done: string, path: string, fn: () => Promise<Problem | null>) {
    busy = true
    failure = null
    try {
      const problem = await tracked('purge', label, fn)
      if (problem) failure = { problem, path, retry: () => run(label, done, path, fn) }
      else notify(done)
    } catch (e) {
      notify(errorText(e))
    } finally {
      busy = false
      ui.revision++
    }
  }
</script>

<div class="view">
  <header>
    <h1>Quarantine</h1>
    <p class="muted">
      Removed items wait here {days} days, then are deleted and their space comes back. Until then, Restore
      puts them back where they were.
    </p>
  </header>
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if !res.data}
    <Busy label="Loading the quarantine" />
  {:else if items.length === 0}
    <p class="muted">Quarantine is empty.</p>
  {:else}
    {#if busy}
      <Busy activity={activities.purge} label="Working" />
    {/if}
    {#if failure}
      <ProblemView problem={failure.problem} path={failure.path} onretry={failure.retry} />
    {/if}
    <div class="toolbar">
      <strong>{plural(items.length, 'item')}, {bytes(total)}</strong>
      <div class="grow"></div>
      <button class="danger" disabled={busy} onclick={emptyAll}>Empty quarantine…</button>
    </div>
    <table class="data-table">
      <thead>
        <tr>
          <th>Item</th>
          <th class="num">Size</th>
          <th>Removed</th>
          <th>Deleted for good</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each items as it (it.id)}
          <tr>
            <td class="path">{it.originalPath}</td>
            <td class="num">{bytes(it.bytes)}</td>
            <td>{dateTime(it.quarantinedAt)}</td>
            <td>{expires(it)}</td>
            <td class="num">
              <button disabled={busy} onclick={() => restore(it)}>Restore</button>
              <button class="quiet danger" disabled={busy} onclick={() => remove(it)}>
                Delete for good…
              </button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

{#if confirm}
  <Modal title={confirm.title} onclose={() => (confirm = null)}>
    <p>{confirm.text} This cannot be undone.</p>
    {#snippet footer()}
      <button onclick={() => (confirm = null)}>Cancel</button>
      <button class="primary" onclick={confirmed}>Delete for good</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .path {
    word-break: break-all;
  }
</style>
