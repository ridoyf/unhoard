<script module lang="ts">
  export type SortKey = 'size' | 'name' | 'files' | 'modified'
</script>

<script lang="ts">
  import type { Node } from '../lib/api'
  import { api } from '../lib/api'
  import { bytes, count, date, percent } from '../lib/format'
  import { askQuarantine, attempt, reveal } from '../lib/state.svelte'
  import ContextMenu, { type MenuItem } from './ContextMenu.svelte'
  import SafetyDot from './SafetyDot.svelte'
  import VirtualList from './VirtualList.svelte'

  let {
    nodes,
    label,
    selectedId = -1,
    onselect,
    onopen,
    onup,
    sort,
    onsort,
    showPath = false,
    showPercent = false,
    checked = $bindable<number[] | undefined>(undefined),
    empty = 'Nothing here.',
  }: {
    nodes: Node[]
    label: string
    selectedId?: number
    onselect?: (n: Node) => void
    onopen?: (n: Node) => void // opening a folder; files open in Explorer
    onup?: () => void
    sort?: { key: SortKey; ascending: boolean }
    onsort?: (key: SortKey) => void
    showPath?: boolean
    showPercent?: boolean
    checked?: number[] // when set, rows get checkboxes
    empty?: string
  } = $props()

  const active = $derived(nodes.findIndex((n) => n.id === selectedId))
  const ticked = $derived(new Set(checked))
  let menu = $state<{ x: number; y: number; node: Node } | null>(null)
  let list = $state<HTMLDivElement | undefined>()
  function select(i: number) {
    const n = nodes[Math.max(0, Math.min(nodes.length - 1, i))]
    if (n) onselect?.(n)
  }

  function open(n: Node) {
    if (n.isDir && n.children && onopen) onopen(n)
    else attempt(api.OpenInExplorer(n.path))
  }

  function key(e: KeyboardEvent) {
    const n = nodes[active]
    switch (e.key) {
      case 'ArrowDown':
        select(active + 1)
        break
      case 'ArrowUp':
        select(active - 1)
        break
      case 'PageDown':
        select(active + 15)
        break
      case 'PageUp':
        select(active - 15)
        break
      case 'Home':
        select(0)
        break
      case 'End':
        select(nodes.length - 1)
        break
      case 'Enter':
        if (n) open(n)
        break
      case 'Backspace':
        onup?.()
        break
      case 'Delete':
        if (n) askQuarantine([n])
        break
      case ' ':
        if (n && checked) toggle(n)
        break
      case 'ContextMenu': {
        if (!n) return
        const r = list?.getBoundingClientRect()
        menu = { x: (r?.left ?? 0) + 40, y: (r?.top ?? 0) + 40, node: n }
        break
      }
      default:
        return
    }
    e.preventDefault()
  }

  function toggle(n: Node) {
    if (!checked) return
    checked = ticked.has(n.id) ? checked.filter((id) => id !== n.id) : [...checked, n.id]
  }

  function showMenu(e: MouseEvent, n: Node) {
    e.preventDefault()
    onselect?.(n)
    menu = { x: e.clientX, y: e.clientY, node: n }
  }

  function closeMenu() {
    menu = null
    list?.focus()
  }

  // The click must not also reach the row, which would change the selection.
  function tick(e: MouseEvent, n: Node) {
    e.stopPropagation()
    toggle(n)
  }

  function menuItems(n: Node): MenuItem[] {
    const items: MenuItem[] = []
    if (n.isDir && n.children && onopen)
      items.push({ label: 'Open folder', run: () => onopen?.(n) })
    if (showPath) items.push({ label: 'Go to folder', run: () => reveal(n.path) })
    items.push(
      { label: 'Show in File Explorer', run: () => attempt(api.OpenInExplorer(n.path)) },
      { label: 'Copy path', run: () => attempt(api.CopyText(n.path), 'Path copied.') },
      {
        label: 'Move to quarantine…',
        danger: true,
        run: () => askQuarantine([n]),
      },
    )
    return items
  }

  const cols: { k: SortKey; text: string; cls: string }[] = [
    { k: 'name', text: 'Name', cls: '' },
    { k: 'size', text: 'Size', cls: 'num' },
    { k: 'files', text: 'Files', cls: 'num' },
    { k: 'modified', text: 'Modified', cls: 'num' },
  ]
</script>

<div class="table" class:check={checked} class:bars={showPercent}>
  <div class="head" role="presentation">
    {#if checked}
      <span></span>
    {/if}
    {#each cols as c}
      {#if onsort}
        <button class="quiet {c.cls}" onclick={() => onsort?.(c.k)}>
          {c.text}{#if sort?.key === c.k}
            <span aria-hidden="true">
              {sort.ascending ? ' ▲' : ' ▼'}
            </span>
            <span class="sr-only">, sorted {sort.ascending ? 'ascending' : 'descending'}</span>
          {/if}
        </button>
      {:else}
        <span class={c.cls}>{c.text}</span>
      {/if}
    {/each}
  </div>
  {#if nodes.length === 0}
    <p class="empty muted">{empty}</p>
  {:else}
    <div
      class="body"
      role="listbox"
      tabindex="0"
      aria-label={label}
      aria-activedescendant={active >= 0 ? `row-${nodes[active].id}` : undefined}
      bind:this={list}
      onkeydown={key}
    >
      <VirtualList items={nodes} rowHeight={showPath ? 44 : 30} key={(n) => n.id} scrollTo={active}>
        {#snippet row(n, i)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <div
            id="row-{n.id}"
            class="row"
            role="option"
            tabindex="-1"
            aria-selected={i === active}
            aria-checked={checked ? ticked.has(n.id) : undefined}
            class:selected={i === active}
            onclick={() => onselect?.(n)}
            ondblclick={() => open(n)}
            oncontextmenu={(e) => showMenu(e, n)}
          >
            {#if checked}
              <!-- The row carries aria-checked and Space toggles it, so the box is for the mouse only. -->
              <input
                type="checkbox"
                checked={ticked.has(n.id)}
                onclick={(e) => tick(e, n)}
                aria-hidden="true"
                tabindex="-1"
              />
            {/if}
            <span class="name">
              <span class="icon" aria-hidden="true">{n.isDir ? (n.link ? '↪' : '▸') : ''}</span>
              <span class="text">
                <span class="title" class:dim={n.hardLink || n.cloud}>
                  {n.name}
                  {#if n.cloud}
                    <span class="tag">online-only</span>
                  {/if}
                  {#if n.hardLink}
                    <span class="tag">hard link</span>
                  {/if}
                  {#if n.link}
                    <span class="tag">link</span>
                  {/if}
                  {#if n.inaccessible}
                    <span class="tag">no access</span>
                  {/if}
                </span>
                {#if showPath}
                  <span class="path muted">{n.path}</span>
                {/if}
              </span>
              {#if n.safety}
                <SafetyDot safety={n.safety} />
              {/if}
            </span>
            <span class="num size">
              {#if showPercent}
                <span class="bar" aria-hidden="true">
                  <span style:width="{Math.min(100, n.percent)}%"></span>
                </span>
                <span class="pct muted">{percent(n.percent)}</span>
              {/if}
              {bytes(n.bytes)}
            </span>
            <span class="num muted">{n.isDir ? count(n.files) : ''}</span>
            <span class="num muted">{date(n.modified)}</span>
          </div>
        {/snippet}
      </VirtualList>
    </div>
  {/if}
</div>

{#if menu}
  <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.node)} onclose={closeMenu} />
{/if}

<style>
  .table {
    --size: 100px;
    --cols: minmax(0, 1fr) var(--size) 80px 110px;
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
    overflow: hidden;
  }
  .bars {
    --size: 210px;
  }
  .check {
    --cols: 28px minmax(0, 1fr) var(--size) 80px 110px;
  }
  .head,
  .row {
    display: grid;
    grid-template-columns: var(--cols);
    align-items: center;
    gap: 8px;
    padding: 0 12px;
  }
  .head {
    border-bottom: 1px solid var(--line);
    color: var(--muted);
    font-size: var(--text-m);
    min-height: 32px;
  }
  .head button {
    padding: 2px 0;
    min-height: 0;
    text-align: inherit;
    color: inherit;
  }
  .head button.num {
    text-align: right;
  }
  .body {
    flex: 1;
    min-height: 0;
  }
  .body:focus-visible {
    outline-offset: -2px;
  }
  .row {
    height: 100%;
    cursor: default;
    border-bottom: 1px solid color-mix(in srgb, var(--line) 50%, transparent);
  }
  .row:hover {
    background: var(--sunken);
  }
  .row.selected {
    background: var(--accent-soft);
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .icon {
    width: 12px;
    color: var(--muted);
    flex: none;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .title,
  .path {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    font-size: var(--text-s);
  }
  .dim {
    color: var(--muted);
  }
  .tag {
    font-size: var(--text-xs);
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: var(--radius-s);
    padding: 0 4px;
    margin-left: 4px;
  }
  .size {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }
  .bar {
    width: 70px;
    height: 6px;
    background: var(--sunken);
    border-radius: 3px;
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
  }
  .pct {
    width: 44px;
    font-size: var(--text-s);
  }
  .empty {
    padding: 24px;
  }
</style>
