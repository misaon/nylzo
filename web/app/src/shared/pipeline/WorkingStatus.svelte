<script lang="ts">
  import { getTranslator } from '../language/translator.svelte.ts';
  import Icon from '../ui/icon/Icon.svelte';
  import type { IconName } from '../ui/icon/icons.ts';

  interface Props {
    readonly label: string;
    readonly icon: IconName;
    readonly progress: number;
    readonly shown: boolean;
  }

  const { label, icon, progress, shown }: Props = $props();
  const translator = getTranslator();
  const format = $derived(new Intl.NumberFormat(translator.language, { style: 'percent', maximumFractionDigits: 0 }));
  const percent = $derived(format.format(progress));
  const widest = $derived(format.format(1));
</script>

<div
  class={[
    'relative flex h-11 items-center justify-between gap-4 overflow-hidden rounded-control bg-white/4 px-4 text-ui font-medium whitespace-nowrap text-ink shadow-rim ring-1 ring-white/12 transition-opacity duration-500 sm:min-w-44 md:h-9 md:px-3.5',
    shown ? 'opacity-100' : 'pointer-events-none opacity-0',
  ]}
  aria-hidden={!shown}
>
  <progress class="absolute inset-0 h-full w-full progress-glow" max="1" value={progress} aria-hidden="true"></progress>
  <span
    class="absolute inset-y-0 left-0 w-1/3 animate-beam bg-linear-to-r from-transparent via-white/8 to-transparent"
    aria-hidden="true"
  ></span>
  <span class="relative flex items-center gap-2">
    <Icon name={icon} class="size-4 shrink-0 text-aegis-200" />
    {label}
  </span>
  <span class="relative stack justify-items-end text-caption text-ink-soft tabular-nums">
    <span class="invisible" aria-hidden="true">{widest}</span>
    <span>{percent}</span>
  </span>
</div>
