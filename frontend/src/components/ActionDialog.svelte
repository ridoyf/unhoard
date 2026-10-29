<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type Plan, type RunResult } from '../lib/api'
  import { activities, tracked } from '../lib/activity.svelte'
  import { slowHint } from '../lib/actions'
  import { bytes, errorText } from '../lib/format'
  import { attempt, notify, ui } from '../lib/state.svelte'
  import Busy from './Busy.svelte'
  import Modal from './Modal.svelte'
  import ProblemView from './ProblemView.svelte'

  let { action, target, onclose }: { action: string; target: string[]; onclose: () => void } =
    $props()

  let plan = $state<Plan | null>(null)
  let error = $state('')
  let running = $state(false)
  let result = $state<RunResult | null>(null)
  let open = true
  onDestroy(() => (open = false))

  $effect(() => {
    tracked('preview', 'Preparing the preview', () => api.PreviewAction(action, target)).then(
      (p) => (plan = p),
      (e) => (error = errorText(e)),
    )
  })

  const freed = $derived(
    plan && plan.bytes > 0
      ? `Frees about ${bytes(plan.bytes)}`
      : 'Space freed: unknown until it runs',
  )

  async function run() {
    if (!plan) return
    const title = plan.title
    running = true
    result = null
    try {
      result = await tracked('action', title, () =>
        api.RunAction(action, target, plan?.commands ?? []),
      )
    } catch (e) {
      error = errorText(e)
    } finally {
      running = false
      ui.revision++
    }
    // Hidden while it ran, so the toast is the only place the outcome can show.
    if (!open) notify(outcome(title))
  }

  function outcome(title: string): string {
    if (error) return `${title} did not finish: ${error}`
    if (result?.ok) return `${title}: done.`
    return `${title} did not finish. ${result?.problem?.summary ?? ''} Open it again for what to do.`
  }
</script>

<Modal title={plan?.title ?? 'Preparing…'} {onclose}>
  {#if error}
    <p class="error-text" role="alert">{error}</p>
  {:else if !plan}
    <Busy
      activity={activities.preview}
      label="Working out what this would do"
      hint="Analysing the Windows component store takes several minutes and prints little while it works."
    />
  {:else if running}
    <Busy activity={activities.action} label={plan.title} hint={slowHint[action]} />
    <p class="muted">
      You can hide this window. The job keeps running, its progress stays at the top of the window,
      and you are told when it finishes.
    </p>
  {:else if result}
    {#if result.ok}
      <p>Done.</p>
    {:else if result.problem}
      <ProblemView problem={result.problem} onretry={run} />
    {/if}
    {#if result.output}
      <details open={!result.ok}>
        <summary class="muted">What it printed</summary>
        <pre>{result.output}</pre>
      </details>
    {/if}
    {#if result.ok}
      <p class="muted">Scan again to see the space it freed.</p>
    {/if}
  {:else}
    <p>{plan.description}</p>
    {#if !plan.available}
      <p class="error-text">{plan.reason}</p>
    {:else}
      {#if plan.targets?.length}
        <div>
          <h3>Affects</h3>
          <ul>
            {#each plan.targets as t}
              <li class="path">{t}</li>
            {/each}
          </ul>
        </div>
      {/if}
      <p>
        <strong>{freed}</strong>
      </p>
      <div>
        <h3>Unhoard will run</h3>
        <pre>{(plan.commands ?? []).join('\n')}</pre>
      </div>
      {#if plan.notes}
        <details>
          <summary>Details</summary>
          <pre>{plan.notes}</pre>
        </details>
      {/if}
    {/if}
  {/if}
  {#snippet footer()}
    {#if running}
      <button class="primary" onclick={onclose}>Hide and keep running</button>
    {:else if result || error || (plan && !plan.available)}
      {#if plan && !plan.available && plan.needsAdmin && !ui.status?.elevated}
        <button onclick={() => attempt(api.RelaunchAsAdmin())}>Restart as administrator</button>
      {/if}
      <button class="primary" onclick={onclose}>Close</button>
    {:else}
      <button onclick={onclose}>Cancel</button>
      <button class="primary" disabled={!plan} onclick={run}>{plan?.title ?? 'Run'}</button>
    {/if}
  {/snippet}
</Modal>

<style>
  pre {
    margin: 4px 0 0;
    padding: 10px 12px;
    background: var(--sunken);
    border-radius: var(--radius-s);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 260px;
    overflow: auto;
  }
  ul {
    margin: 4px 0 0;
    padding-left: 18px;
  }
  .path {
    word-break: break-all;
  }
</style>
