<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  import { tweenedNumber } from './motion.svelte.ts';
  import { FILLS } from './tones.ts';
  import type { Tone } from './tones.ts';

  interface Props {
    readonly value: number;
    readonly tone: Tone;
    readonly reserve?: number | undefined;
    readonly class?: ClassValue;
  }

  const SPAN = 1000;

  const { value, tone, reserve, class: className }: Props = $props();
  const filled = tweenedNumber(() => Math.min(1, Math.max(0, value)));
  const reserveStart = $derived(SPAN * (1 - Math.min(1, Math.max(0, reserve ?? 0))));
</script>

<span class={['relative block h-1.5 overflow-hidden rounded-full bg-white/8', className]}>
  <svg class="absolute inset-0 size-full" viewBox="0 0 {SPAN} 1" preserveAspectRatio="none" aria-hidden="true">
    {#if reserve !== undefined && reserve > 0}
      <rect x={reserveStart} width={SPAN - reserveStart} height="1" class="fill-rose-400/30"></rect>
    {/if}
    <rect width={filled.current * SPAN} height="1" class={FILLS[tone]}></rect>
    {#if reserve !== undefined && reserve > 0}
      <rect x={reserveStart - 3} width="6" height="1" class="fill-rose-300"></rect>
    {/if}
  </svg>
</span>
