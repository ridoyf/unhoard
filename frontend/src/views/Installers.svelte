<script lang="ts">
  import { api } from '../lib/api'
  import Choice from '../components/Choice.svelte'
  import FinderPage from '../components/FinderPage.svelte'

  let days = $state(30)
</script>

<FinderPage
  title="Old installers"
  intro="Setup programs and disk images in Downloads. Once an app is installed, you can usually download its installer again if you need it."
  load={() => api.OldInstallers(days)}
  empty="No old installers in Downloads."
>
  {#snippet controls()}
    <Choice
      label="Not opened for"
      bind:value={days}
      options={[
        [0, 'any time'],
        [30, '1 month'],
        [90, '3 months'],
        [365, '1 year'],
      ]}
    />
  {/snippet}
</FinderPage>
