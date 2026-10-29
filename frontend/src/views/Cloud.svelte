<script lang="ts">
  import { api } from '../lib/api'
  import { askAction } from '../lib/state.svelte'
  import Choice from '../components/Choice.svelte'
  import FinderPage from '../components/FinderPage.svelte'

  let minMB = $state(10)
</script>

<FinderPage
  title="Cloud copies"
  intro="OneDrive files with a copy on this PC. Online-only frees the copy. The file stays in OneDrive and downloads again when you open it."
  load={() => api.CloudCopies(minMB)}
  action={{
    label: 'Make online-only…',
    run: (picked) =>
      askAction(
        'cloud-online-only',
        picked.map((n) => n.path),
      ),
  }}
  empty="No OneDrive files are stored locally, or OneDrive is not set up."
>
  {#snippet controls()}
    <Choice
      label="At least"
      bind:value={minMB}
      options={[
        [1, '1 MB'],
        [10, '10 MB'],
        [100, '100 MB'],
        [1024, '1 GB'],
      ]}
    />
  {/snippet}
</FinderPage>
