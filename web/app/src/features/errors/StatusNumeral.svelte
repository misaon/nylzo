<script lang="ts">
  import StatusOrb from './StatusOrb.svelte';

  interface Props {
    readonly first: string;
    readonly last: string;
    readonly tone: 'current' | 'danger';
    readonly glyph: 'compass' | 'server';
  }

  const { first, last, tone, glyph }: Props = $props();
</script>

<div
  class="flex animate-rise items-center justify-center font-display text-[120px] leading-none font-extralight tracking-[-0.04em] tabular-nums sm:text-[216px]"
  aria-hidden="true"
>
  <span class="numeral-glass">{first}</span>
  <StatusOrb {tone} {glyph} class="z-10 -mx-2 size-[92px] sm:-mx-4 sm:size-[150px]" />
  <span class="numeral-glass">{last}</span>
</div>
