<script lang="ts">
  import { api, type Node, type TypeStat } from '../lib/api'
  import { bytes, count } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { go, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import Inspector from '../components/Inspector.svelte'
  import NodeList from '../components/NodeList.svelte'

  const tabs = [
    { id: 'files', label: 'Largest files' },
    { id: 'folders', label: 'Largest folders' },
    { id: 'types', label: 'File types' },
  ] as const
  let tab = $state<(typeof tabs)[number]['id']>('files')
  let selected = $state<Node | null>(null)

  const res = loader<{ nodes: Node[]; types: TypeStat[] }>(async () => {
    if (tab === 'types') return { nodes: [], types: (await api.FileTypes()) ?? [] }
    return { nodes: (await api.Largest(tab === 'files', 100)) ?? [], types: [] }
  })
  function typeLabel(ext: string): string {
    if (ext === '') return '(no extension)'
    return ext === '(other)' ? 'Everything else' : `.${ext}`
  }

  function findType(ext: string) {
    ui.searchExt = ext
    go('search')
  }

  const typeMax = $derived(Math.max(1, ...(res.data?.types ?? []).map((t) => t.bytes)))
</script>

<div class="view fill">
  <header>
    <h1>Largest and file types</h1>
  </header>
  <div class="toolbar">
    {#each tabs as t}
      <button aria-pressed={tab === t.id} class:primary={tab === t.id} onclick={() => (tab = t.id)}>
        {t.label}
      </button>
    {/each}
  </div>
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if !res.data}
    <Busy label="Looking through the scan" />
  {:else if tab === 'types'}
    <table class="data-table">
      <thead>
        <tr>
          <th>Type</th>
          <th class="num">Files</th>
          <th class="num">Size on disk</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each res.data.types as t}
          <tr>
            <td>{typeLabel(t.ext)}</td>
            <td class="num">{count(t.files)}</td>
            <td class="num">{bytes(t.bytes)}</td>
            <td class="barcell">
              <span class="bar"><span style:width="{(t.bytes / typeMax) * 100}%"></span></span>
              {#if t.ext && t.ext !== '(other)'}
                <button class="quiet" onclick={() => findType(t.ext)}>Find</button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {:else}
    <div class="split">
      <NodeList
        nodes={res.data.nodes}
        label={tab === 'files' ? 'Largest files' : 'Largest folders'}
        showPath
        selectedId={selected?.id ?? -1}
        onselect={(n) => (selected = n)}
      />
      <Inspector node={selected} />
    </div>
  {/if}
</div>

<style>
  table {
    max-width: 920px;
  }
  .barcell {
    width: 40%;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .bar {
    flex: 1;
    height: 8px;
    background: var(--sunken);
    border-radius: var(--radius-s);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
  }
</style>
