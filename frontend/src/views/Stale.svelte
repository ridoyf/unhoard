<script lang="ts">
  import { api } from '../lib/api'
  import Choice from '../components/Choice.svelte'
  import FinderPage from '../components/FinderPage.svelte'

  let minMB = $state(100)
  let days = $state(365)
</script>

<FinderPage
  title="Big and stale"
  intro="Large files nobody has changed in a long time. These are your own files, so look before you remove anything."
  load={() => api.BigStale(minMB, days)}
  empty="No large files are that old."
>
  {#snippet controls()}
    <Choice
      label="At least"
      bind:value={minMB}
      options={[
        [50, '50 MB'],
        [100, '100 MB'],
        [500, '500 MB'],
        [1024, '1 GB'],
        [5120, '5 GB'],
      ]}
    />
    <Choice
      label="untouched for"
      bind:value={days}
      options={[
        [90, '3 months'],
        [180, '6 months'],
        [365, '1 year'],
        [730, '2 years'],
        [1825, '5 years'],
      ]}
    />
  {/snippet}
</FinderPage>
