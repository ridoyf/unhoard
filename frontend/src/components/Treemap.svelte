<script lang="ts">
  import type { Tile } from '../lib/api'
  import { bytes, safetyLabel } from '../lib/format'

  let {
    tiles,
    selectedId = -1,
    onselect,
    onopen,
    onresize,
  }: {
    tiles: Tile[]
    selectedId?: number
    onselect: (id: number) => void
    onopen: (id: number) => void
    onresize: (w: number, h: number) => void
  } = $props()

  let w = $state(0)
  let h = $state(0)
  // Waits for resizing to settle before asking the backend for a new layout.
  $effect(() => {
    const width = w
    const height = h
    if (width <= 0 || height <= 0) return
    const timer = setTimeout(() => onresize(width, height), 120)
    return () => clearTimeout(timer)
  })

  const selected = $derived(tiles.find((t) => t.node && t.node.id === selectedId))

  function tint(t: Tile): string {
    const s = t.node?.safety || 'unknown'
    return `var(--${s})`
  }

  function title(t: Tile): string {
    const rating = t.node?.safety
      ? `\n${safetyLabel[t.node.safety]}${t.node.ruleName ? ': ' + t.node.ruleName : ''}`
      : ''
    return `${t.node?.path ?? t.label}\n${bytes(t.bytes)}${rating}`
  }
</script>

<div class="map" bind:clientWidth={w} bind:clientHeight={h} aria-hidden="true">
  {#each tiles as t, i (i)}
    <!-- The list beside the map is the keyboard-accessible equivalent. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="tile d{t.depth}"
      class:dir={t.node?.isDir}
      class:other={!t.node}
      class:rated={!!t.node?.safety}
      style:left="{t.x}px"
      style:top="{t.y}px"
      style:width="{t.w}px"
      style:height="{t.h}px"
      style:--tint={tint(t)}
      title={title(t)}
      onclick={() => t.node && onselect(t.node.id)}
      ondblclick={() => t.node?.isDir && t.node.children && onopen(t.node.id)}
    >
      {#if t.w > 54 && t.h > 16}
        <span class="label">{t.label}</span>
        {#if t.h > 34 && t.depth === 0}
          <span class="size">{bytes(t.bytes)}</span>
        {/if}
      {/if}
    </div>
  {/each}
  {#if selected}
    <div
      class="ring"
      style:left="{selected.x}px"
      style:top="{selected.y}px"
      style:width="{selected.w}px"
      style:height="{selected.h}px"
    ></div>
  {/if}
</div>
<div class="legend">
  {#each ['safe', 'rebuilds', 'delete-via-app', 'never'] as s}
    <span>
      <i style:background="var(--{s})"></i>
      {safetyLabel[s]}
    </span>
  {/each}
  <span>
    <i class="none"></i>
    Not recognised
  </span>
</div>

<style>
  .map {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    background: var(--sunken);
    border-radius: var(--radius-m);
  }
  .tile {
    position: absolute;
    overflow: hidden;
    padding: 2px 5px;
    font-size: var(--text-s);
    line-height: 1.3;
    border: 1px solid var(--surface);
    background: color-mix(in srgb, var(--tint) 14%, var(--surface));
    cursor: default;
  }
  .tile.rated {
    background: color-mix(in srgb, var(--tint) 34%, var(--surface));
  }
  .tile.d1 {
    background: color-mix(in srgb, var(--tint) 10%, var(--surface));
    border-color: color-mix(in srgb, var(--line) 60%, transparent);
  }
  .tile.d1.rated {
    background: color-mix(in srgb, var(--tint) 45%, var(--surface));
  }
  .tile.other {
    background: repeating-linear-gradient(135deg, var(--sunken) 0 4px, var(--surface) 4px 8px);
    color: var(--muted);
  }
  .tile:hover {
    filter: brightness(0.96);
  }
  /* Drawn on top as its own layer so it never hides a folder's nested tiles. */
  .ring {
    position: absolute;
    border: 2px solid var(--accent);
    pointer-events: none;
    z-index: 3;
  }
  .label,
  .size {
    display: block;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .d0.dir > .label {
    font-weight: 600;
  }
  .size {
    color: var(--muted);
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    font-size: var(--text-s);
    color: var(--muted);
  }
  .legend span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .legend i {
    width: 10px;
    height: 10px;
    border-radius: 2px;
  }
  .legend i.none {
    border: 1px solid var(--unknown);
  }
</style>
