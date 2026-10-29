<script lang="ts">
  import { bytes, dateTime } from '../lib/format'

  let { points, label }: { points: { at: string; bytes: number }[]; label: string } = $props()

  let w = $state(600)
  const h = 180
  const pad = { l: 64, r: 16, t: 12, b: 26 }

  const xs = $derived(points.map((p) => new Date(p.at).getTime()))
  const ys = $derived(points.map((p) => p.bytes))
  const x0 = $derived(Math.min(...xs))
  const x1 = $derived(Math.max(...xs))
  const yLo = $derived(Math.min(...ys) * 0.95)
  const yHi = $derived(Math.max(...ys) * 1.05 || 1)

  function px(t: number) {
    return x1 === x0
      ? (pad.l + w - pad.r) / 2
      : pad.l + ((t - x0) / (x1 - x0)) * (w - pad.l - pad.r)
  }
  function py(v: number) {
    return pad.t + (1 - (v - yLo) / (yHi - yLo || 1)) * (h - pad.t - pad.b)
  }

  const path = $derived(
    points
      .map((p, i) => `${i ? 'L' : 'M'}${px(xs[i]).toFixed(1)},${py(p.bytes).toFixed(1)}`)
      .join(''),
  )
  const ticks = $derived([yLo, (yLo + yHi) / 2, yHi])

  let hover = $state(-1)
  function move(e: PointerEvent) {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect()
    const x = e.clientX - r.left
    let best = -1
    let d = Infinity
    xs.forEach((t, i) => {
      const dd = Math.abs(px(t) - x)
      if (dd < d) [d, best] = [dd, i]
    })
    hover = best
  }
</script>

<figure>
  <div class="wrap" bind:clientWidth={w}>
    <svg
      width={w}
      height={h}
      role="img"
      aria-label="{label}: line chart"
      onpointermove={move}
      onpointerleave={() => (hover = -1)}
    >
      {#each ticks as t}
        <line class="grid" x1={pad.l} x2={w - pad.r} y1={py(t)} y2={py(t)} />
        <text class="axis" x={pad.l - 8} y={py(t) + 4} text-anchor="end">{bytes(t)}</text>
      {/each}
      {#if points.length > 1}
        <path d={path} class="line" />
      {/if}
      {#each points as p, i}
        <circle cx={px(xs[i])} cy={py(p.bytes)} r={points.length > 40 ? 0 : 3} class="dot" />
      {/each}
      {#if hover >= 0}
        <line class="cross" x1={px(xs[hover])} x2={px(xs[hover])} y1={pad.t} y2={h - pad.b} />
        <circle cx={px(xs[hover])} cy={py(ys[hover])} r="5" class="dot hot" />
      {/if}
      {#if points.length}
        <text class="axis" x={pad.l} y={h - 6}>{dateTime(points[0].at)}</text>
        {#if points.length > 1}
          <text class="axis" x={w - pad.r} y={h - 6} text-anchor="end">
            {dateTime(points[points.length - 1].at)}
          </text>
        {/if}
      {/if}
    </svg>
    {#if hover >= 0}
      <div
        class="tip"
        style:left="{Math.min(px(xs[hover]) + 10, w - 170)}px"
        style:top="{Math.max(0, py(ys[hover]) - 46)}px"
      >
        <strong>{bytes(ys[hover])}</strong>
        <br />
        <span class="muted">{dateTime(points[hover].at)}</span>
      </div>
    {/if}
  </div>
  <table class="sr-only">
    <caption>{label}</caption>
    <tbody>
      {#each points as p}
        <tr>
          <td>{dateTime(p.at)}</td>
          <td>{bytes(p.bytes)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
</figure>

<style>
  figure {
    margin: 0;
  }
  .wrap {
    position: relative;
  }
  svg {
    display: block;
    touch-action: none;
  }
  .grid {
    stroke: var(--line);
    stroke-width: 1;
  }
  .axis {
    fill: var(--muted);
    font-size: var(--text-xs);
  }
  .line {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .dot {
    fill: var(--accent);
    stroke: var(--surface);
    stroke-width: 2;
  }
  .cross {
    stroke: var(--muted);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }
  .tip {
    position: absolute;
    pointer-events: none;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-s);
    padding: 6px 10px;
    font-size: var(--text-s);
    white-space: nowrap;
    box-shadow: 0 4px 12px rgb(0 0 0 / 0.12);
  }
</style>
