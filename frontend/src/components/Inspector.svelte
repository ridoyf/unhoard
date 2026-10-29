<script lang="ts">
  import { api, type Node } from '../lib/api'
  import { bytes, count, date } from '../lib/format'
  import { openAction, pickerView, takesPath } from '../lib/actions'
  import { loader } from '../lib/load.svelte'
  import { askQuarantine, attempt } from '../lib/state.svelte'
  import Busy from './Busy.svelte'
  import SafetyDot from './SafetyDot.svelte'

  let { node }: { node: Node | null } = $props()

  const res = loader(async () => {
    const path = node?.path
    return path ? { path, explanation: await api.Explain(path) } : null
  })
  // The loader keeps the last answer while the next one loads. A safety verdict must
  // never show against a different item, so it is only used once it matches the node.
  const info = $derived(res.data && res.data.path === node?.path ? res.data.explanation : null)
  const error = $derived(res.error)

  const action = $derived(info?.rule?.action ?? '')
  const opensPicker = $derived(!!pickerView[action] && !takesPath(action))

  function runAction() {
    if (!action || !node) return
    openAction(action, node.path)
  }
</script>

<aside class="inspector" aria-label="What is this?">
  {#if !node}
    <p class="muted">Select something to see what it is and whether it is safe to remove.</p>
  {:else}
    <div class="title">
      <h2>{node.name}</h2>
      <p class="muted path">{node.path}</p>
    </div>

    <dl class="facts">
      <dt>Size on disk</dt>
      <dd>{bytes(node.bytes)}</dd>
      {#if node.logical !== node.bytes}
        <dt>Size</dt>
        <dd>{bytes(node.logical)}</dd>
      {/if}
      {#if node.isDir}
        <dt>Files</dt>
        <dd>{count(node.files)}</dd>
      {/if}
      {#if node.modified > 0}
        <dt>Modified</dt>
        <dd>{date(node.modified)}</dd>
      {/if}
    </dl>
    {#if node.hardLink}
      <p class="note">
        This is an extra hard link to a file stored elsewhere, so its space is counted there.
      </p>
    {/if}
    {#if node.cloud}
      <p class="note">Online-only: the file lives in the cloud and uses almost no space here.</p>
    {/if}
    {#if node.link}
      <p class="note">A link to another folder. Its contents are counted where they really are.</p>
    {/if}
    {#if node.inaccessible}
      <p class="note">
        Windows would not let you read this folder. Restarting as administrator may show its
        contents.
      </p>
    {/if}

    {#if error}
      <p class="note error-text" role="alert">{error}</p>
    {:else if info}
      <section class="what">
        {#if info.rule}
          <SafetyDot safety={info.rule.safety} label />
          <h3>{info.rule.name}</h3>
          {#if info.inherited}
            <p class="muted">
              Part of <span class="path">{info.rulePath}</span>
            </p>
          {/if}
          <p>{info.rule.description}</p>
          <p class="muted">Belongs to {info.rule.owner}</p>
          {#if info.rule.learnMore}
            <button
              class="quiet link"
              onclick={() => attempt(api.OpenURL(info?.rule?.learnMore ?? ''))}
            >
              Learn more
            </button>
          {/if}
        {:else}
          <SafetyDot safety="" label />
          <p class="muted">
            Not recognised. If you did not create it, leave it alone or check what owns it first.
          </p>
        {/if}
      </section>

      <div class="actions">
        {#if action}
          <button class="primary" onclick={runAction}>
            {opensPicker ? pickerView[action].label : `${info.actionTitle}…`}
          </button>
        {/if}
        {#if info.canDelete}
          <button class:primary={!action} onclick={() => node && askQuarantine([node])}>
            Move to quarantine…
          </button>
        {:else}
          <p class="blocked">Cannot be removed: {info.blockReason}.</p>
        {/if}
        <div class="row">
          <button onclick={() => node && attempt(api.OpenInExplorer(node.path))}>
            Show in File Explorer
          </button>
          <button onclick={() => node && attempt(api.CopyText(node.path), 'Path copied.')}>
            Copy path
          </button>
        </div>
      </div>
    {:else}
      <Busy label="Checking what this is" compact />
    {/if}
  {/if}
</aside>

<style>
  .inspector {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 16px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
    overflow: auto;
    min-height: 0;
  }
  .title h2 {
    word-break: break-word;
  }
  .path {
    word-break: break-all;
    font-size: var(--text-s);
  }
  .facts {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 14px;
    margin: 0;
  }
  dt {
    color: var(--muted);
  }
  dd {
    margin: 0;
  }
  .note {
    font-size: var(--text-m);
    color: var(--muted);
  }
  .what {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 12px;
    border-top: 1px solid var(--line);
  }
  .actions {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .actions .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .blocked {
    font-size: var(--text-m);
    padding: 8px 10px;
    border-left: 3px solid var(--never);
    background: color-mix(in srgb, var(--never) 8%, var(--surface));
  }
  .link {
    align-self: flex-start;
    color: var(--accent);
    padding-left: 0;
  }
</style>
