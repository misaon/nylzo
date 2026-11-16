<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly rows: number;
    readonly class?: ClassValue;
  }

  const { rows, class: className }: Props = $props();
</script>

<div class={['grid gap-3', className]} aria-hidden="true">
  {#each { length: rows }, row (row)}
    <span class="block h-6 animate-pulse rounded-xs bg-white/8"></span>
  {/each}
</div>
