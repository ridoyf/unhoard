<script lang="ts" generics="T extends string | number">
  let {
    label,
    value = $bindable(),
    options,
    disabled = false,
  }: {
    label: string
    value: T
    options: [T, string][]
    disabled?: boolean
  } = $props()
</script>

<label>
  {label}
  <select bind:value {disabled}>
    {#each options as [v, text] (v)}
      <option value={v}>{text}</option>
    {/each}
  </select>
</label>
