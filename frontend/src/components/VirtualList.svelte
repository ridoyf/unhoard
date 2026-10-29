<script lang="ts" generics="T">
  import type { Snippet } from 'svelte'

  let {
    items,
    rowHeight = 30,
    row,
    key,
    scrollTo = -1,
  }: {
    items: T[]
    rowHeight?: number
    row: Snippet<[T, number]>
    key: (item: T) => string | number
    scrollTo?: number // keep this index in view
  } = $props()

  let viewport = $state<HTMLDivElement | undefined>()
  let scrollTop = $state(0)
  let height = $state(0)

  const start = $derived(Math.max(0, Math.floor(scrollTop / rowHeight) - 8))
  const end = $derived(Math.min(items.length, Math.ceil((scrollTop + height) / rowHeight) + 8))
  const visible = $derived(items.slice(start, end))

  $effect(() => {
    if (!viewport || scrollTo < 0) return
    const top = scrollTo * rowHeight
    if (top < viewport.scrollTop) viewport.scrollTop = top
    else if (top + rowHeight > viewport.scrollTop + height)
      viewport.scrollTop = top + rowHeight - height
  })
</script>

<div
  class="viewport"
  bind:this={viewport}
  bind:clientHeight={height}
  onscroll={() => (scrollTop = viewport?.scrollTop ?? 0)}
>
  <div class="spacer" style:height="{items.length * rowHeight}px">
    {#each visible as item, i (key(item))}
      <div class="slot" style:top="{(start + i) * rowHeight}px" style:height="{rowHeight}px">
        {@render row(item, start + i)}
      </div>
    {/each}
  </div>
</div>

<style>
  .viewport {
    overflow-y: auto;
    height: 100%;
    contain: strict;
  }
  .spacer {
    position: relative;
  }
  .slot {
    position: absolute;
    left: 0;
    right: 0;
  }
</style>
