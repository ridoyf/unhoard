<script lang="ts">
  import { api } from '../lib/api'
  import FinderPage from '../components/FinderPage.svelte'
</script>

<FinderPage
  title="Games"
  intro="Games in Steam, Epic, Xbox, GOG, EA, Ubisoft and Riot libraries. Uninstall them from their launcher or from Apps, so the launcher knows they are gone."
  load={() => api.Games()}
  checkable={false}
  empty="No game libraries found on this drive."
/>
