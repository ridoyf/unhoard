<script lang="ts">
  import type { Snippet } from 'svelte'

  let {
    title,
    onclose,
    busy = false,
    children,
    footer,
  }: {
    title: string
    onclose: () => void
    busy?: boolean
    children: Snippet
    footer: Snippet
  } = $props()

  let dialog = $state<HTMLDialogElement | undefined>()
  $effect(() => {
    dialog?.showModal()
  })
</script>

<!-- While busy, Esc must not close the dialog: its result would land on an unmounted component. -->
<dialog
  bind:this={dialog}
  {onclose}
  oncancel={(e) => busy && e.preventDefault()}
  aria-labelledby="modal-title"
>
  <h2 id="modal-title">{title}</h2>
  <div class="content">{@render children()}</div>
  <footer>{@render footer()}</footer>
</dialog>

<style>
  dialog {
    width: min(680px, calc(100vw - 48px));
    max-height: calc(100vh - 64px);
    padding: 0;
    border: 1px solid var(--line);
    border-radius: var(--radius-l);
    background: var(--surface);
    color: var(--ink);
    box-shadow: 0 20px 60px rgb(0 0 0 / 0.3);
    display: flex;
    flex-direction: column;
  }
  dialog::backdrop {
    background: rgb(10 14 20 / 0.45);
  }
  h2 {
    padding: 20px 22px 0;
  }
  .content {
    padding: 14px 22px;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 14px 22px;
    border-top: 1px solid var(--line);
  }
</style>
