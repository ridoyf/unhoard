<script lang="ts">
  import { api } from '../lib/api'
  import { ui } from '../lib/state.svelte'
  import FinderPage from '../components/FinderPage.svelte'

  const dev = $derived(ui.view === 'dev')
</script>

{#if dev}
  <FinderPage
    title="Developer junk"
    intro="Package folders, build output and tool caches. Their tools rebuild them when a project needs them. Items marked delete via app need their cleanup tool."
    load={() => api.RuleFindings('dev')}
    empty="No developer folders found."
  />
{:else}
  <FinderPage
    title="Caches and temp files"
    intro="Browser and app caches and temporary files. Apps rebuild them, so the next start may be a little slower."
    load={() => api.RuleFindings('caches')}
    empty="No caches found."
  />
{/if}
