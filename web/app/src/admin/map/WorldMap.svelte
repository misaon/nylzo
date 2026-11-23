<script lang="ts">
  import { level } from '../charts/geometry/scale.ts';
  import { loadWorldMap } from './world-map.ts';

  interface Props {
    readonly label: string;
    readonly visitors: Readonly<Record<string, number>>;
    readonly highlighted: string | undefined;
    readonly onhighlight: (country: string | undefined) => void;
  }

  const LEVELS = [
    'fill-white/[0.045]',
    'fill-aegis-500/30',
    'fill-aegis-500/45',
    'fill-aegis-400/55',
    'fill-aegis-400/70',
    'fill-aegis-300/80',
    'fill-aegis-300/88',
    'fill-aegis-300',
  ] as const;

  const { label, visitors, highlighted, onhighlight }: Props = $props();
  const id = $props.id();
  const map = loadWorldMap();
  const largest = $derived(Math.max(0, ...Object.values(visitors)));

  function tone(country: string): string {
    const shade = level(visitors[country] ?? 0, largest, LEVELS.length - 1);
    let fill: string = LEVELS[0];
    for (const [index, candidate] of LEVELS.entries()) {
      if (index === shade) {
        fill = candidate;
      }
    }
    return fill;
  }

  function point(event: PointerEvent): void {
    const target = event.target;
    if (target instanceof SVGPathElement) {
      const country = target.dataset['country'];
      onhighlight(country !== undefined && (visitors[country] ?? 0) > 0 ? country : undefined);
    }
  }
</script>

<div class="relative aspect-[1000/440] w-full" role="img" aria-label={label}>
  {#await map then world}
    <svg
      class="absolute inset-0 size-full animate-appear overflow-visible"
      viewBox={`0 0 ${world.width} ${world.height}`}
      aria-hidden="true"
      focusable="false"
    >
      <defs>
        <filter id="{id}-glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="4"></feGaussianBlur>
        </filter>
      </defs>
      <g
        onpointerover={point}
        onpointerleave={(event) => {
          if (event.pointerType === 'mouse') {
            onhighlight(undefined);
          }
        }}
        role="presentation"
      >
        {#each Object.entries(world.countries) as [country, shape] (country)}
          <path
            d={shape}
            data-country={country}
            class={[tone(country), 'stroke-white/12 transition-[fill] duration-700']}
            stroke-width="0.6"
            stroke-linejoin="round"
          ></path>
        {/each}
      </g>
      {#if highlighted !== undefined && Object.hasOwn(world.countries, highlighted)}
        <g class="pointer-events-none animate-appear">
          <path d={world.countries[highlighted]} class="fill-aegis-200" opacity="0.9" filter="url(#{id}-glow)"></path>
          <path d={world.countries[highlighted]} class="fill-white stroke-white" stroke-width="1.5"></path>
        </g>
      {/if}
    </svg>
  {/await}
</div>
