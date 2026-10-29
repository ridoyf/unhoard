<script lang="ts">
  import { untrack } from 'svelte'
  import { api } from '../lib/api'
  import { bytes, count, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { attempt, notify, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import Inspector from '../components/Inspector.svelte'
  import NodeList, { type SortKey } from '../components/NodeList.svelte'
  import Treemap from '../components/Treemap.svelte'

  // shortcut: a folder shows its first 5,000 entries in the chosen order; page on scroll if folders that big matter.
  const pageSize = 5000

  let sort = $state<{ key: SortKey; ascending: boolean }>({ key: 'size', ascending: false })
  let size = $state({ w: 0, h: 0 })

  const listing = loader(() =>
    Promise.all([
      api.NodeWithPath(ui.folder),
      api.Children(ui.folder, sort.key, sort.ascending, 0, pageSize),
    ]),
  )
  const map = loader(async () =>
    size.w > 0 ? ((await api.Treemap(ui.folder, size.w, size.h)) ?? []) : [],
  )

  const crumbs = $derived(listing.data?.[0] ?? [])
  const children = $derived(listing.data?.[1].items ?? [])
  const total = $derived(listing.data?.[1].total ?? 0)
  const folder = $derived(crumbs[crumbs.length - 1])

  // A folder that is gone (quarantined, or replaced by a new scan) falls back to the scan's root.
  $effect(() => {
    if (!listing.error) return
    notify(listing.error)
    untrack(() => {
      if (ui.scan && ui.folder !== ui.scan.rootId) ui.folder = ui.scan.rootId
    })
  })

  $effect(() => {
    const ids = new Set(children.map((c) => c.id))
    untrack(() => {
      if (ui.selected && !ids.has(ui.selected.id)) ui.selected = null
    })
  })

  function open(id: number) {
    ui.folder = id
    ui.selected = null
  }

  function up() {
    if (crumbs.length > 1) {
      ui.selected = crumbs[crumbs.length - 1]
      ui.folder = crumbs[crumbs.length - 2].id
    }
  }

  function selectId(id: number) {
    const n = children.find((c) => c.id === id)
    if (n) ui.selected = n
    else
      attempt(
        api.NodeWithPath(id).then((chain) => (ui.selected = chain?.[chain.length - 1] ?? null)),
      )
  }

  function sortBy(key: SortKey) {
    sort = { key, ascending: sort.key === key ? !sort.ascending : key === 'name' }
  }
</script>

<div class="explore">
  <div class="crumbs">
    <nav aria-label="Folder path">
      {#each crumbs as c, i}
        {#if i > 0}
          <span class="sep" aria-hidden="true">›</span>
        {/if}
        {#if i === crumbs.length - 1}
          <span aria-current="page">{c.name}</span>
        {:else}
          <button class="quiet" onclick={() => open(c.id)}>{c.name}</button>
        {/if}
      {/each}
    </nav>
    {#if folder}
      <span class="muted">{bytes(folder.bytes)}</span>
    {/if}
    {#if listing.loading}
      <Busy label="Opening" compact />
    {/if}
  </div>

  <section class="map" aria-label="Safety map">
    <Treemap
      tiles={map.data ?? []}
      selectedId={ui.selected?.id ?? -1}
      onselect={selectId}
      onopen={open}
      onresize={(w, h) => (size = { w, h })}
    />
  </section>

  <div class="lower">
    <div class="list">
      <NodeList
        nodes={children}
        label="Contents of {folder?.name ?? 'folder'}"
        selectedId={ui.selected?.id ?? -1}
        onselect={(n) => (ui.selected = n)}
        onopen={(n) => open(n.id)}
        onup={up}
        {sort}
        onsort={sortBy}
        showPercent
        empty="This folder is empty."
      />
      {#if total > children.length}
        <p class="muted more">
          Showing {count(children.length)} of {plural(total, 'item')}.
        </p>
      {/if}
    </div>
    <Inspector node={ui.selected} />
  </div>
</div>

<style>
  .explore {
    display: grid;
    grid-template-rows: auto minmax(180px, 40%) minmax(0, 1fr);
    gap: 12px;
    height: 100%;
    padding: 14px 20px 18px;
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .crumbs nav {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .crumbs button {
    padding: 2px 6px;
    min-height: 0;
  }
  .crumbs [aria-current] {
    font-weight: 600;
    padding: 2px 6px;
  }
  .sep {
    color: var(--muted);
  }
  .map {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
  }
  .lower {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 320px;
    gap: 12px;
    min-height: 0;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-height: 0;
  }
  .more {
    font-size: var(--text-s);
  }
</style>
