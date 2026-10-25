<script lang="ts">
  interface Props {
    readonly label: string;
    readonly progress: number;
    readonly fill?: number;
    readonly shown: boolean;
    readonly announced?: boolean;
  }

  const { label, progress, fill = progress, shown, announced = false }: Props = $props();
</script>

<div
  class={[
    'relative flex h-11 items-center justify-center gap-2.5 overflow-hidden rounded-[9px] bg-aegis-950/70 px-5 text-sm font-medium whitespace-nowrap text-ink ring-1 ring-aegis-400/45 transition-opacity duration-500 md:h-9 md:px-4',
    shown ? 'opacity-100' : 'pointer-events-none opacity-0',
  ]}
  aria-hidden={!announced || !shown}
>
  <progress class="absolute inset-0 h-full w-full progress-fill" max="1" value={fill} aria-hidden="true"></progress>
  <span
    class="absolute inset-y-0 left-0 w-1/3 animate-beam bg-linear-to-r from-transparent via-white/15 to-transparent"
    aria-hidden="true"
  ></span>
  <svg class="relative size-4 shrink-0 animate-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-opacity="0.25" stroke-width="2.2"></circle>
    <path d="M20.5 12A8.5 8.5 0 0 0 12 3.5" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"></path>
  </svg>
  <span class="relative">{label}</span>
  <span class="relative inline-block w-[4ch] font-mono text-[13px] whitespace-nowrap text-white/75 tabular-nums"
    >{Math.round(progress * 100)} %</span
  >
</div>
