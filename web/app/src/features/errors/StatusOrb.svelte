<script lang="ts">
  import type { ClassValue } from 'svelte/elements';

  interface Props {
    readonly tone: 'current' | 'danger' | 'warning';
    readonly glyph: 'compass' | 'server' | 'wrench';
    readonly class: ClassValue;
    readonly orbit?: boolean;
  }

  const TONES = {
    current: { tile: 'tile-current', halo: 'bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.55),transparent)]' },
    danger: { tile: 'tile-danger', halo: 'bg-[radial-gradient(closest-side,oklch(0.56_0.17_18/0.5),transparent)]' },
    warning: { tile: 'tile-warning', halo: 'bg-[radial-gradient(closest-side,oklch(0.7_0.14_75/0.42),transparent)]' },
  } as const;

  const { tone, glyph, class: className, orbit = false }: Props = $props();
</script>

<span class={['relative grid shrink-0 place-items-center rounded-full', className]} aria-hidden="true">
  <span class={['pointer-events-none absolute -inset-[42%] rounded-full motion-safe:animate-halo', TONES[tone].halo]}
  ></span>
  {#if orbit}
    <span
      class="pointer-events-none absolute -inset-[24%] rounded-full border border-dashed border-[oklch(0.88_0.09_85/0.3)] motion-safe:animate-orbit"
    >
      <span
        class="absolute top-0 left-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-[oklch(0.92_0.1_88)] shadow-[0_0_14px_3px_oklch(0.82_0.14_80/0.75)]"
      ></span>
    </span>
  {/if}
  <span class={['absolute inset-0 rounded-full backdrop-blur-md', TONES[tone].tile]}></span>
  <span
    class="pointer-events-none absolute inset-x-[16%] top-[5%] h-[38%] rounded-[50%] bg-linear-to-b from-white/26 to-white/0 blur-[1px]"
  ></span>
  <span class="ring-light"><span class="ring-light-spin animate-spin-slow"></span></span>
  <svg
    class="relative size-[44%] overflow-visible text-white drop-shadow-[0_2px_10px_oklch(0.18_0.08_265/0.7)]"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    {#if glyph === 'compass'}
      <circle cx="12" cy="12" r="9"></circle>
      <g class="origin-[12px_12px] [transform-box:view-box] motion-safe:animate-seek">
        <path d="M12 6l2.4 6H9.6z" fill="currentColor"></path>
        <path d="M9.6 12h4.8L12 18z" stroke-opacity="0.6"></path>
      </g>
    {:else if glyph === 'server'}
      <rect x="4" y="4.5" width="16" height="6.5" rx="2"></rect>
      <rect x="4" y="13" width="16" height="6.5" rx="2"></rect>
      <path d="M11 7.75h5.5M11 16.25h5.5" stroke-opacity="0.55"></path>
      <circle cx="7.5" cy="7.75" r="1" fill="currentColor" stroke="none"></circle>
      <circle cx="7.5" cy="16.25" r="1.2" fill="currentColor" stroke="none" class="motion-safe:animate-led"></circle>
    {:else}
      <g class="origin-[16px_8px] [transform-box:view-box] motion-safe:animate-wrench">
        <path
          d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"
        ></path>
      </g>
    {/if}
  </svg>
</span>
