<script lang="ts">
  import { onMount, type Component } from 'svelte'
  import { api } from './lib/api'
  import { activities } from './lib/activity.svelte'
  import { bytes, count, duration, plural } from './lib/format'
  import { attempt, go, init, startScan, ui, type View } from './lib/state.svelte'
  import ActionDialog from './components/ActionDialog.svelte'
  import Busy from './components/Busy.svelte'
  import QuarantineDialog from './components/QuarantineDialog.svelte'
  import Home from './views/Home.svelte'
  import Explore from './views/Explore.svelte'
  import FreeUp from './views/FreeUp.svelte'
  import Findings from './views/Findings.svelte'
  import Duplicates from './views/Duplicates.svelte'
  import Stale from './views/Stale.svelte'
  import Installers from './views/Installers.svelte'
  import Games from './views/Games.svelte'
  import Cloud from './views/Cloud.svelte'
  import Largest from './views/Largest.svelte'
  import Search from './views/Search.svelte'
  import Apps from './views/Apps.svelte'
  import Hidden from './views/Hidden.svelte'
  import Tools from './views/Tools.svelte'
  import History from './views/History.svelte'
  import Quarantine from './views/Quarantine.svelte'
  import Log from './views/Log.svelte'
  import Settings from './views/Settings.svelte'

  type Entry = { view: View; label: string; needsScan?: boolean }
  const nav: { group: string; items: Entry[] }[] = [
    {
      group: '',
      items: [
        { view: 'home', label: 'Drives' },
        { view: 'explore', label: 'Explore', needsScan: true },
        { view: 'free', label: 'Free up space', needsScan: true },
      ],
    },
    {
      group: 'Find',
      items: [
        { view: 'caches', label: 'Caches and temp files', needsScan: true },
        { view: 'dev', label: 'Developer junk', needsScan: true },
        { view: 'duplicates', label: 'Duplicates', needsScan: true },
        { view: 'stale', label: 'Big and stale', needsScan: true },
        { view: 'installers', label: 'Old installers', needsScan: true },
        { view: 'games', label: 'Games', needsScan: true },
        { view: 'cloud', label: 'Cloud copies', needsScan: true },
        { view: 'largest', label: 'Largest and file types', needsScan: true },
        { view: 'search', label: 'Search', needsScan: true },
      ],
    },
    {
      group: 'Understand',
      items: [
        { view: 'apps', label: 'Apps' },
        { view: 'hidden', label: 'Hidden space', needsScan: true },
        { view: 'history', label: 'History' },
      ],
    },
    {
      group: 'Clean up',
      items: [
        { view: 'tools', label: 'Cleanup tools' },
        { view: 'quarantine', label: 'Quarantine' },
        { view: 'log', label: 'Action log' },
      ],
    },
  ]

  const views: Record<View, Component> = {
    home: Home,
    explore: Explore,
    free: FreeUp,
    caches: Findings,
    dev: Findings,
    duplicates: Duplicates,
    stale: Stale,
    installers: Installers,
    games: Games,
    cloud: Cloud,
    largest: Largest,
    search: Search,
    apps: Apps,
    hidden: Hidden,
    tools: Tools,
    history: History,
    quarantine: Quarantine,
    log: Log,
    settings: Settings,
  }
  const needsScan = new Set(
    nav.flatMap((g) => g.items.filter((i) => i.needsScan).map((i) => i.view)),
  )
  const Current = $derived(needsScan.has(ui.view) && !ui.scan ? Home : views[ui.view])

  // An invisible left-to-right mark after a path. The path is drawn right-to-left so its
  // end stays visible when it is cut off, and without the mark the trailing "\" of a
  // drive root would move to the front ("\C:" instead of "C:\").
  const ltr = '\u200e'

  const running = $derived(Object.values(activities))
  // Read out when a job starts or ends; the progress itself would be too chatty to announce.
  const announced = $derived(
    [ui.scanning ? 'Scanning' : '', ...running.map((a) => a.label)].filter(Boolean).join(', '),
  )

  onMount(() => {
    init()
  })
</script>

<div class="shell">
  <nav aria-label="Main">
    <div class="brand">Unhoard</div>
    {#each nav as g}
      <div class="group">
        {#if g.group}
          <div class="group-name">{g.group}</div>
        {/if}
        {#each g.items as item}
          <button
            class="nav-item"
            class:current={ui.view === item.view}
            aria-current={ui.view === item.view ? 'page' : undefined}
            disabled={item.needsScan && !ui.scan}
            onclick={() => go(item.view)}
          >
            {item.label}
          </button>
        {/each}
      </div>
    {/each}
    <div class="grow"></div>
    <button
      class="nav-item"
      class:current={ui.view === 'settings'}
      aria-current={ui.view === 'settings' ? 'page' : undefined}
      onclick={() => go('settings')}
    >
      Settings
    </button>
  </nav>

  <div class="main">
    <div class="bar">
      <span class="sr-only" role="status">{announced}</span>
      {#if ui.scanning}
        <div class="progress">
          <span class="pulse" aria-hidden="true"></span>
          <span>
            Scanning{ui.progress?.method === 'mft' ? ' (fast)' : ''}: {count(
              ui.progress?.files ?? 0,
            )} files, {bytes(ui.progress?.bytes ?? 0)}
            <span class="muted">({duration(ui.progress?.elapsedMs ?? 0)})</span>
          </span>
          <span class="current muted">{(ui.progress?.current ?? '') + ltr}</span>
          <button onclick={() => attempt(api.CancelScan())}>Cancel</button>
        </div>
      {:else if ui.scan}
        <span class="scanned">
          <strong>{ui.scan.root + ltr}</strong>
          <span class="muted">
            {bytes(ui.scan.bytes)} in {count(ui.scan.files)} files, scanned in {duration(
              ui.scan.durationMs,
            )}{ui.scan.method === 'mft' ? ' (fast scan)' : ''}
          </span>
          {#if ui.scan.errors > 0}
            <span class="muted" title="Restarting as administrator reads most of them.">
              {plural(ui.scan.errors, 'folder')} could not be read
            </span>
          {/if}
        </span>
        <button onclick={() => ui.scan && startScan(ui.scan.root)}>Scan again</button>
      {:else}
        <span class="muted">No drive scanned yet.</span>
        <div class="grow"></div>
      {/if}
      {#if ui.update?.available}
        <button
          class="quiet"
          title="Opens the release page on GitHub"
          onclick={() => attempt(api.OpenURL(ui.update?.url ?? ''))}
        >
          Version {ui.update.latest} is available
        </button>
      {/if}
      {#if ui.status && !ui.status.elevated}
        <button
          class="quiet"
          title="Admin rights allow a much faster scan and show hidden system space"
          onclick={() => attempt(api.RelaunchAsAdmin())}
        >
          Restart as administrator
        </button>
      {/if}
    </div>
    {#if running.length}
      <div class="activities" aria-label="Running jobs">
        {#each running as a (a.id)}
          <Busy activity={a} compact />
        {/each}
      </div>
    {/if}
    {#if ui.scanError}
      <div class="error" role="alert">
        {ui.scanError}
        <button class="quiet" onclick={() => (ui.scanError = '')}>Dismiss</button>
      </div>
    {/if}
    <main>
      {#key ui.view}
        <Current />
      {/key}
    </main>
  </div>
</div>

<!-- The live region stays in the page so screen readers announce each new notice. -->
<div role="status">
  {#if ui.notice}
    <div class="toast">{ui.notice}</div>
  {/if}
</div>

{#if ui.dialog?.kind === 'quarantine'}
  <QuarantineDialog items={ui.dialog.items} onclose={() => (ui.dialog = null)} />
{:else if ui.dialog?.kind === 'action'}
  <ActionDialog
    action={ui.dialog.action}
    target={ui.dialog.target}
    onclose={() => (ui.dialog = null)}
  />
{/if}

<style>
  .shell {
    display: grid;
    grid-template-columns: 196px minmax(0, 1fr);
    height: 100vh;
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 10px;
    border-right: 1px solid var(--line);
    overflow-y: auto;
  }
  .brand {
    font: 600 20px/1 var(--font-display);
    letter-spacing: -0.02em;
    padding: 4px 10px 6px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .group-name {
    font-size: var(--text-s);
    color: var(--muted);
    padding: 0 10px 4px;
  }
  .nav-item {
    border: none;
    background: none;
    text-align: left;
    padding: 3px 10px;
    min-height: 27px;
    border-radius: var(--radius-s);
  }
  .nav-item:hover:not(:disabled) {
    background: var(--sunken);
  }
  .nav-item.current {
    background: var(--accent-soft);
    font-weight: 600;
    box-shadow: inset 3px 0 0 var(--accent);
  }
  .main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 28px;
    border-bottom: 1px solid var(--line);
    min-height: 54px;
    background: var(--surface);
  }
  .scanned {
    display: flex;
    gap: 10px;
    align-items: baseline;
    min-width: 0;
    flex: 1;
  }
  .scanned > * {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .scanned strong {
    flex: 0 1 auto;
    direction: rtl; /* keep the end of a long folder path visible */
  }
  .bar button {
    white-space: nowrap;
  }
  .progress {
    display: flex;
    align-items: center;
    gap: 12px;
    flex: 1;
    min-width: 0;
  }
  .current {
    flex: 1;
    min-width: 0;
    font-size: var(--text-s);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
  }
  .pulse {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.2s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.25;
    }
  }
  .activities {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 28px;
    padding: 8px 28px;
    border-bottom: 1px solid var(--line);
    background: var(--surface);
    font-size: var(--text-m);
  }
  .error {
    padding: 8px 28px;
    background: color-mix(in srgb, var(--never) 12%, var(--surface));
    border-bottom: 1px solid var(--line);
  }
  main {
    flex: 1;
    min-height: 0;
  }
  .toast {
    position: fixed;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--ink);
    color: var(--paper);
    padding: 10px 16px;
    border-radius: var(--radius-m);
    z-index: 60;
    max-width: 70ch;
  }
</style>
