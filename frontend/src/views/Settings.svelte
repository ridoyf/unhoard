<script lang="ts">
  import { api, type Settings } from '../lib/api'
  import { errorText } from '../lib/format'
  import { tracked } from '../lib/activity.svelte'
  import Busy from '../components/Busy.svelte'
  import { applyTheme, attempt, notify, ui } from '../lib/state.svelte'

  const themes = [
    { id: 'auto', label: 'Match Windows' },
    { id: 'light', label: 'Light' },
    { id: 'dark', label: 'Dark' },
  ] as const

  // Loaded once rather than with loader(), so a scan finishing does not reset unsaved edits.
  let s = $state<Settings | null>(null)
  let loadError = $state('')
  let updateError = $state('')
  let checking = $state(false)

  $effect(() => {
    api.Settings().then(
      (x) => (s = x),
      (e) => (loadError = errorText(e)),
    )
  })

  async function save(e: SubmitEvent) {
    e.preventDefault()
    if (!s) return
    try {
      await api.SaveSettings(s)
      notify('Settings saved.')
    } catch (err) {
      notify(errorText(err))
    }
  }

  async function checkUpdate() {
    updateError = ''
    checking = true
    try {
      ui.update = await api.CheckForUpdate()
    } catch (e) {
      updateError = errorText(e)
    } finally {
      checking = false
    }
  }

  async function exportReport(format: string) {
    try {
      const path = await tracked('export', 'Exporting a report', () => api.ExportReport(format))
      if (path) notify(`Report saved to ${path}.`)
    } catch (e) {
      notify(errorText(e))
    }
  }
</script>

<div class="view">
  <header><h1>Settings</h1></header>

  <section>
    <h2>Appearance</h2>
    <div class="toolbar" role="group" aria-label="Theme">
      {#each themes as t}
        <button
          aria-pressed={ui.theme === t.id}
          class:primary={ui.theme === t.id}
          onclick={() => applyTheme(t.id)}
        >
          {t.label}
        </button>
      {/each}
    </div>
  </section>

  {#if loadError}
    <p class="error-text" role="alert">{loadError}</p>
  {:else if s}
    <form onsubmit={save}>
      <section>
        <h2>Quarantine</h2>
        <label>
          Delete quarantined items for good after <input
            type="number"
            min="1"
            max="365"
            bind:value={s.retentionDays}
          />
          days
        </label>
      </section>
      <section>
        <h2>Running in the background</h2>
        <label class="check">
          <input type="checkbox" bind:checked={s.background} />
          Keep Unhoard in the notification area when the window is closed
        </label>
        <label>
          Warn me when a drive has less than <input
            type="number"
            min="0"
            max="50"
            bind:value={s.lowSpacePercent}
          />
          % free (0 turns this off)
        </label>
        <label>
          Scan <input
            class="wide"
            bind:value={s.scheduledRoot}
            aria-label="Drive to scan on a schedule"
          />
          every
          <input type="number" min="0" max="720" bind:value={s.scanEveryHours} />
          hours (0 turns this off; needs background mode)
        </label>
        <p class="muted small">
          Alerts and scheduled scans only happen while Unhoard is running. Scheduled scans add to
          History so you can see what grew.
        </p>
      </section>
      <section>
        <h2>Updates</h2>
        <label class="check">
          <input type="checkbox" bind:checked={s.checkUpdates} />
          Check GitHub for a new version once a day
        </label>
        <p class="muted small">
          This only asks GitHub for the latest version number. Nothing is downloaded or installed,
          and nothing about you or your files is sent.
        </p>
        <div class="toolbar">
          <button type="button" onclick={checkUpdate} disabled={checking}>Check now</button>
          {#if checking}
            <Busy label="Asking GitHub for the latest version" compact />
          {:else if ui.update}
            {#if ui.update.available}
              <span>Version {ui.update.latest} is available.</span>
              <button
                type="button"
                class="primary"
                onclick={() => attempt(api.OpenURL(ui.update?.url ?? ''))}
              >
                Open download page
              </button>
            {:else}
              <span class="muted">You have the latest version ({ui.update.current}).</span>
            {/if}
          {:else if updateError}
            <span class="muted">Could not check: {updateError}</span>
          {/if}
        </div>
      </section>
      <div class="toolbar"><button type="submit" class="primary">Save settings</button></div>
    </form>
  {/if}

  <section>
    <h2>Reports</h2>
    <p class="muted small">
      Save the loaded scan's top-level folders, 100 largest files and file types.
    </p>
    <div class="toolbar">
      <button disabled={!ui.scan} onclick={() => exportReport('csv')}>Export CSV…</button>
      <button disabled={!ui.scan} onclick={() => exportReport('html')}>Export HTML…</button>
    </div>
  </section>

  <section>
    <h2>About</h2>
    <p class="muted small">
      Unhoard {ui.status?.version}. {ui.status?.rules} built-in rules. Data, logs and quarantine live
      in {ui.status?.dataDir}. Rule files placed in its rules folder override the built-in ones.
    </p>
  </section>
</div>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 760px;
    padding-bottom: 16px;
    border-bottom: 1px solid var(--line);
  }
  label {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  input[type='number'] {
    width: 76px;
  }
  .wide {
    width: 90px;
  }
</style>
