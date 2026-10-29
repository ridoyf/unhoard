<script module lang="ts">
  import type { Node } from '../lib/api'

  export type SelectionAction = { label: string; run: (picked: Node[]) => void }
</script>

<script lang="ts">
  import { bytes } from '../lib/format'
  import { askQuarantine } from '../lib/state.svelte'

  const quarantine: SelectionAction = {
    label: 'Move selected to quarantine…',
    run: (picked) => askQuarantine(picked),
  }

  let {
    nodes,
    checked = $bindable(),
    action = quarantine,
  }: { nodes: Node[]; checked: number[]; action?: SelectionAction } = $props()

  // "Select all" never picks items the knowledge base says to leave or to remove through their app.
  const selectable = $derived(
    nodes.filter((n) => n.safety !== 'never' && n.safety !== 'delete-via-app'),
  )
  const ticked = $derived(new Set(checked))
  const picked = $derived(nodes.filter((n) => ticked.has(n.id)))
  const total = $derived(picked.reduce((s, n) => s + n.bytes, 0))
  const all = $derived(selectable.length > 0 && selectable.every((n) => ticked.has(n.id)))
</script>

<div class="toolbar">
  <button
    disabled={selectable.length === 0}
    onclick={() => (checked = all ? [] : selectable.map((n) => n.id))}
  >
    {all ? 'Clear selection' : 'Select all'}
  </button>
  <span class="muted">{picked.length} selected, {bytes(total)}</span>
  <div class="grow"></div>
  <button class="primary" disabled={picked.length === 0} onclick={() => action.run(picked)}>
    {action.label}
  </button>
</div>
