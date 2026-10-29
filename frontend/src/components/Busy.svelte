<script lang="ts">
  import type { Activity } from '../lib/activity.svelte'
  import { clock } from '../lib/format'

  let {
    label = '',
    activity = null,
    hint = '',
    compact = false,
  }: {
    label?: string
    activity?: Activity | null
    hint?: string // what a long silence usually means for this job
    compact?: boolean
  } = $props()

  // Silence longer than this is worth explaining.
  const stallMs = 30_000
  // Elapsed time is only shown once a job is clearly not instant.
  const showClockMs = 2_000

  const mountedAt = Date.now()
  let now = $state(Date.now())
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(timer)
  })

  const elapsed = $derived(now - (activity?.startedAt ?? mountedAt))
  const quiet = $derived(activity ? now - activity.changedAt : 0)
  const percent = $derived(activity && activity.percent >= 0 ? activity.percent : -1)
  const name = $derived(activity?.label ?? label)
</script>

<div class="busy" class:compact title={compact ? activity?.detail : undefined}>
  <div
    class="track"
    class:indeterminate={percent < 0}
    role="progressbar"
    aria-label={name}
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent >= 0 ? Math.floor(percent) : undefined}
  >
    <span style:width={percent >= 0 ? `${percent}%` : undefined}></span>
  </div>
  <p class="line">
    <span>{name}</span>
    {#if percent >= 0}
      <span>{Math.floor(percent)}%</span>
    {/if}
    {#if elapsed >= showClockMs}
      <span class="muted">{clock(elapsed)}</span>
    {/if}
    {#if compact && quiet > stallMs}
      <span class="warn">no change for {clock(quiet)}</span>
    {/if}
  </p>
  {#if !compact && activity?.detail}
    <p class="detail muted">{activity.detail}</p>
  {/if}
  {#if !compact && quiet > stallMs}
    <p class="stalled">
      No change for {clock(quiet)}.
      {hint ||
        'Some steps take a long time without reporting anything, and the job is still running. If nothing changes for several more minutes, restarting Windows clears most stuck operations.'}
    </p>
  {/if}
</div>

<style>
  .busy {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .compact {
    flex-direction: row;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .track {
    position: relative;
    height: 8px;
    background: var(--sunken);
    border-radius: var(--radius-s);
    overflow: hidden;
  }
  .compact .track {
    width: 64px;
    flex: none;
    height: 6px;
  }
  .track span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.4s ease-out;
  }
  .indeterminate span {
    position: absolute;
    width: 35%;
    animation: slide 1.4s ease-in-out infinite;
  }
  @keyframes slide {
    from {
      left: -35%;
    }
    to {
      left: 100%;
    }
  }
  .line {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: baseline;
  }
  .compact .line {
    flex-wrap: nowrap;
    white-space: nowrap;
  }
  .detail {
    font-size: var(--text-m);
    word-break: break-all;
  }
  .warn {
    color: var(--delete-via-app);
  }
  .stalled {
    padding: 8px 10px;
    border-left: 3px solid var(--delete-via-app);
    background: color-mix(in srgb, var(--delete-via-app) 10%, var(--surface));
  }
</style>
