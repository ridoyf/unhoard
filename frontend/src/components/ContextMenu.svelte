<script module lang="ts">
  export type MenuItem = { label: string; run: () => void; danger?: boolean }
</script>

<script lang="ts">
  let { x, y, items, onclose }: { x: number; y: number; items: MenuItem[]; onclose: () => void } =
    $props()

  let menu = $state<HTMLDivElement | undefined>()
  $effect(() => {
    menu?.querySelector<HTMLButtonElement>('button')?.focus()
  })

  function key(e: KeyboardEvent) {
    const buttons = [...(menu?.querySelectorAll<HTMLButtonElement>('button') ?? [])]
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement)
    // Tab would move focus out while the menu and its backdrop stay open.
    if (e.key === 'Escape' || e.key === 'Tab') onclose()
    else if (e.key === 'ArrowDown') buttons[(i + 1) % buttons.length]?.focus()
    else if (e.key === 'ArrowUp') buttons[(i - 1 + buttons.length) % buttons.length]?.focus()
    else return
    e.preventDefault()
  }

  function choose(item: MenuItem) {
    onclose()
    item.run()
  }

  function dismiss(e: MouseEvent) {
    e.preventDefault()
    onclose()
  }

  const menuWidth = 230
  const itemHeight = 32
  const padding = 16
  const left = $derived(Math.min(x, window.innerWidth - menuWidth))
  const top = $derived(Math.min(y, window.innerHeight - items.length * itemHeight - padding))
</script>

<!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
<div class="backdrop" onclick={onclose} oncontextmenu={dismiss}></div>
<div
  class="menu"
  role="menu"
  tabindex="-1"
  bind:this={menu}
  style:left="{left}px"
  style:top="{top}px"
  onkeydown={key}
>
  {#each items as item}
    <button role="menuitem" class:danger={item.danger} onclick={() => choose(item)}>
      {item.label}
    </button>
  {/each}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }
  .menu {
    position: fixed;
    z-index: 41;
    min-width: 210px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
    padding: 4px;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.18);
    display: flex;
    flex-direction: column;
  }
  button {
    border: none;
    background: none;
    text-align: left;
    border-radius: var(--radius-s);
  }
  button:hover,
  button:focus-visible {
    background: var(--sunken);
    outline: none;
  }
  .danger {
    color: var(--never);
  }
</style>
