<script lang="ts">
  import { api, type LoggedAction } from '../lib/api'
  import { bytes, dateTime } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import Busy from '../components/Busy.svelte'

  const res = loader<LoggedAction[]>(async () => (await api.ActionLog(500)) ?? [])
  const entries = $derived(res.data)

  const names: Record<string, string> = {
    quarantine: 'Moved to quarantine',
    restore: 'Restored',
    purge: 'Deleted for good',
  }
</script>

<div class="view">
  <header>
    <h1>Action log</h1>
    <p class="muted">
      Every change made on this PC, newest first, with the commands run and anything that failed.
    </p>
  </header>
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if entries === null}
    <Busy label="Loading the action log" />
  {:else if entries.length === 0}
    <p class="muted">Nothing yet.</p>
  {:else}
    <ol>
      {#each entries as e (e.id)}
        <li class:failed={!e.ok}>
          <div class="head">
            <strong>{names[e.action] ?? e.action}</strong>
            <span class="muted">{dateTime(e.at)}</span>
            {#if e.bytes}
              <span>{bytes(e.bytes)}</span>
            {/if}
            <span class="status">{e.ok ? 'Done' : 'Failed'}</span>
          </div>
          {#each e.paths ?? [] as p}
            <div class="path">{p}</div>
          {/each}
          {#if e.result}
            <pre>{e.result}</pre>
          {/if}
        </li>
      {/each}
    </ol>
  {/if}
</div>

<style>
  ol {
    list-style: none;
    margin: 0;
    padding: 0;
    max-width: 1000px;
  }
  li {
    padding: 10px 0 10px 12px;
    border-bottom: 1px solid var(--line);
    border-left: 3px solid var(--line);
    margin-bottom: 4px;
  }
  li.failed {
    border-left-color: var(--never);
  }
  .head {
    display: flex;
    gap: 12px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .status {
    margin-left: auto;
    font-size: var(--text-m);
    color: var(--muted);
  }
  .failed .status {
    color: var(--never);
  }
  .path {
    word-break: break-all;
    font-size: var(--text-m);
  }
  pre {
    margin: 4px 0 0;
    white-space: pre-wrap;
    color: var(--muted);
  }
</style>
