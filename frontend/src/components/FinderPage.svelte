<script lang="ts">
  import { untrack, type Snippet } from 'svelte'
  import type { Node } from '../lib/api'
  import { bytes, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import Busy from './Busy.svelte'
  import Inspector from './Inspector.svelte'
  import NodeList from './NodeList.svelte'
  import SelectionBar, { type SelectionAction } from './SelectionBar.svelte'

  let {
    title,
    intro,
    load,
    controls,
    checkable = true,
    action,
    empty = 'Nothing found.',
  }: {
    title: string
    intro: string
    load: () => Promise<Node[] | null> // re-run whenever reactive values it reads change
    controls?: Snippet
    checkable?: boolean
    action?: SelectionAction // what the selection bar offers; quarantine by default
    empty?: string
  } = $props()

  const res = loader(async () => (await load()) ?? [])
  const nodes = $derived(res.data)
  let checked = $state<number[]>([])
  let selected = $state<Node | null>(null)

  // Fresh results drop ticks and the selection for items that are gone.
  $effect(() => {
    const ids = new Set((nodes ?? []).map((n) => n.id))
    untrack(() => {
      checked = checked.filter((id) => ids.has(id))
      if (selected && !ids.has(selected.id)) selected = null
    })
  })

  const total = $derived((nodes ?? []).reduce((s, n) => s + n.bytes, 0))
</script>

<div class="view fill">
  <header>
    <h1>{title}</h1>
    <p class="muted">{intro}</p>
  </header>
  {#if controls}
    <div class="toolbar">{@render controls()}</div>
  {/if}
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if nodes === null}
    <Busy label="Looking through the scan" />
  {:else}
    <div class="toolbar">
      <strong>{plural(nodes.length, 'item')}, {bytes(total)}</strong>
      {#if res.loading}
        <Busy label="Updating" compact />
      {/if}
    </div>
    {#if checkable && nodes.length > 0}
      <SelectionBar {nodes} {action} bind:checked />
    {/if}
    <div class="split">
      <NodeList
        {nodes}
        label={title}
        showPath
        selectedId={selected?.id ?? -1}
        onselect={(n) => (selected = n)}
        bind:checked={() => (checkable ? checked : undefined), (v) => (checked = v ?? [])}
        {empty}
      />
      <Inspector node={selected} />
    </div>
  {/if}
</div>
