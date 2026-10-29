<script lang="ts">
  import { api, type Filter, type Node } from '../lib/api'
  import { bytes, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import Choice from '../components/Choice.svelte'
  import Inspector from '../components/Inspector.svelte'
  import NodeList from '../components/NodeList.svelte'
  import SelectionBar from '../components/SelectionBar.svelte'

  let text = $state('')
  let pathText = $state('')
  let exts = $state(ui.searchExt)
  let minMB = $state(0)
  let kind = $state('')
  let older = $state(0)
  let newer = $state(0)

  // The search runs when the form is submitted, not on every keystroke, so the loader
  // watches the submitted filter rather than the fields.
  let query = $state<Filter | null>(null)
  const res = loader(async () => (query ? ((await api.Search(query)) ?? []) : null))
  const nodes = $derived(res.data)
  let checked = $state<number[]>([])
  let selected = $state<Node | null>(null)

  function search(e?: SubmitEvent) {
    e?.preventDefault()
    ui.searchExt = ''
    checked = []
    query = {
      text,
      pathText,
      kind,
      minBytes: Math.max(0, minMB) * 1024 * 1024,
      exts: exts.split(/[,\s]+/).filter(Boolean),
      olderThanDays: older,
      newerThanDays: newer,
    }
  }

  // Arriving from File types with a type filled in searches straight away.
  if (ui.searchExt) search()
</script>

<div class="view fill">
  <header>
    <h1>Search</h1>
    <p class="muted">Up to 1,000 results, biggest first.</p>
  </header>
  <form class="filters" onsubmit={search}>
    <label>
      Name contains <input bind:value={text} placeholder="e.g. backup" />
    </label>
    <label>
      Path contains <input bind:value={pathText} placeholder="e.g. Videos" />
    </label>
    <label>
      Types <input bind:value={exts} placeholder="e.g. mp4, mkv" />
    </label>
    <Choice
      label="Show"
      bind:value={kind}
      options={[
        ['', 'Files and folders'],
        ['files', 'Files'],
        ['folders', 'Folders'],
      ]}
    />
    <Choice
      label="At least"
      bind:value={minMB}
      options={[
        [0, 'any size'],
        [1, '1 MB'],
        [100, '100 MB'],
        [1024, '1 GB'],
      ]}
    />
    <Choice
      label="Changed"
      bind:value={older}
      options={[
        [0, 'any time'],
        [30, 'over a month ago'],
        [365, 'over a year ago'],
      ]}
    />
    <Choice
      label="or"
      bind:value={newer}
      options={[
        [0, 'any time'],
        [1, 'in the last day'],
        [7, 'in the last week'],
        [30, 'in the last month'],
      ]}
    />
    <button class="primary" type="submit">Search</button>
  </form>
  {#if res.loading}
    <Busy label="Searching the scan" compact />
  {/if}
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if nodes}
    <p>
      <strong>
        {plural(nodes.length, 'result')}, {bytes(nodes.reduce((s, n) => s + n.bytes, 0))}
      </strong>
    </p>
    {#if nodes.length}
      <SelectionBar {nodes} bind:checked />
    {/if}
    <div class="split">
      <NodeList
        {nodes}
        label="Search results"
        showPath
        selectedId={selected?.id ?? -1}
        onselect={(n) => (selected = n)}
        bind:checked
        empty="Nothing matches."
      />
      <Inspector node={selected} />
    </div>
  {/if}
</div>

<style>
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 16px;
    align-items: end;
  }
  .filters :global(label) {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: var(--text-m);
    color: var(--muted);
  }
  .filters :global(:is(input, select)) {
    color: var(--ink);
  }
</style>
