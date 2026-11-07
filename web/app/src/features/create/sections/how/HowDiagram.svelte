<script lang="ts">
  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import { DIAGRAM_NODES, LAYOUTS } from './diagram.ts';
  import type { DiagramLayout } from './diagram.ts';

  interface Props {
    readonly layout: DiagramLayout;
  }

  const { layout }: Props = $props();
  const { translate } = getTranslator();
  const shape = $derived(LAYOUTS[layout]);
</script>

<div class={['relative w-full', shape.frame]} role="img" aria-label={translate('how.diagram')}>
  <svg
    class="absolute inset-0 size-full rv-sweep overflow-visible [--rv-delay:80ms]"
    viewBox={shape.viewBox}
    fill="none"
    aria-hidden="true"
    focusable="false"
  >
    <defs>
      <linearGradient
        id="keyArc{shape.id}"
        gradientUnits="userSpaceOnUse"
        x1={shape.arcFrom}
        y1="0"
        x2={shape.arcTo}
        y2="0"
      >
        <stop offset="0" stop-color="#98D5FF"></stop>
        <stop offset="0.5" stop-color="#B4A0F2"></stop>
        <stop offset="1" stop-color="#98D5FF"></stop>
      </linearGradient>
      <mask id="keyArc{shape.id}Gap" maskUnits="userSpaceOnUse" x="-50" y="-50" width="1100" height="420">
        <rect x="-50" y="-50" width="1100" height="420" fill="white"></rect>
        <rect
          x={shape.gap.x}
          y={shape.gap.y}
          width={shape.gap.width}
          height={shape.gap.height}
          rx={shape.gap.radius}
          fill="black"
        ></rect>
      </mask>
      <filter id="keyArc{shape.id}Glow" filterUnits="userSpaceOnUse" x="-50" y="-50" width="1100" height="420">
        <feGaussianBlur stdDeviation="3"></feGaussianBlur>
      </filter>
    </defs>
    <g mask="url(#keyArc{shape.id}Gap)">
      <path
        d={shape.arc}
        stroke="url(#keyArc{shape.id})"
        stroke-width={shape.glowWidth}
        stroke-linecap="round"
        opacity="0.35"
        filter="url(#keyArc{shape.id}Glow)"
      ></path>
      <path
        d={shape.arc}
        stroke="url(#keyArc{shape.id})"
        stroke-width={shape.arcWidth}
        stroke-linecap="round"
        opacity="0.95"
      ></path>
      {#each ['stroke-nebula-400/35', 'stroke-nebula-300'] as tone, index (tone)}
        <path
          d={shape.arc}
          pathLength="100"
          stroke-dasharray="4 200"
          stroke-dashoffset="4"
          class={[tone, 'motion-safe:animate-how4-flow-key']}
          stroke-width={index === 0 ? 10 : 3}
          stroke-linecap="round"
        ></path>
      {/each}
    </g>
    {#each shape.ends as end (end.x)}
      <circle cx={end.x} cy={end.y} r={shape.endHalo} fill="#98D5FF" opacity="0.14"></circle>
      <circle cx={end.x} cy={end.y} r={shape.endDot} fill="#98D5FF"></circle>
    {/each}
    {#each shape.lines as line (line)}
      <path d={line} class="stroke-aegis-400/45" stroke-width="2" stroke-linecap="round"></path>
    {/each}
    {#each shape.lines as line, index (line)}
      {#each ['stroke-aegis-300/35', 'stroke-aegis-200'] as tone, layer (tone)}
        <path
          d={line}
          pathLength="100"
          stroke-dasharray="8 200"
          stroke-dashoffset="8"
          class={[tone, index === 0 ? 'motion-safe:animate-how4-flow-a' : 'motion-safe:animate-how4-flow-b']}
          stroke-width={layer === 0 ? 10 : 3}
          stroke-linecap="round"
        ></path>
      {/each}
    {/each}
  </svg>
  {#each shape.chips as chip (chip.position)}
    <span
      class={[
        'absolute inline-flex h-5 -translate-x-1/2 -translate-y-1/2 items-center gap-1.5 rounded-full bg-void-950 bg-[linear-gradient(oklch(0.56_0.19_258/0.14),oklch(0.56_0.19_258/0.14))] px-2 text-micro whitespace-nowrap text-aegis-200 ring-1 ring-aegis-400/35',
        chip.animation,
        chip.position,
      ]}
    >
      <Icon name="lock" class={shape.chipIcon} stroke={shape.chipStroke} />
      {translate('how.chip.cipher')}
    </span>
  {/each}
  <span
    class={[
      'absolute inline-flex h-7 -translate-x-1/2 -translate-y-1/2 items-center gap-1.5 rounded-full bg-void-950 bg-[linear-gradient(180deg,oklch(0.72_0.14_305/0.34),oklch(0.72_0.14_305/0.14))] px-3 text-caption font-medium whitespace-nowrap text-ink shadow-[inset_0_1px_0_oklch(1_0_0/0.18),0_0_24px_-4px_oklch(0.72_0.14_305/0.85)] ring-1 ring-nebula-300/60 motion-safe:animate-how4-chip-key',
      shape.keyChip,
    ]}
  >
    <Icon name="key" class="size-3.5 shrink-0 text-nebula-300" stroke={shape.chipStroke} />
    {translate('how.chip.key')}
  </span>
  {#each DIAGRAM_NODES as node, index (node.title)}
    <div class={['absolute -translate-x-1/2 -translate-y-1/2 rv-pop', node.delay, shape.nodes[index]]}>
      <span
        class="absolute -inset-[45%] rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.28),transparent)]"
      ></span>
      <span
        class={[
          'relative grid aspect-square w-full place-items-center rounded-full text-ink-soft glass-pop',
          node.animation,
        ]}
      >
        <Icon name={node.icon} class="size-[40%]" stroke={1.6} />
      </span>
      {#if node.badge}
        <span
          class={[
            'absolute -top-[6%] -right-[6%] grid size-[36%] place-items-center rounded-full text-white opacity-0 step-current outline-[3px] outline-void-950',
            node.badge.animation,
          ]}
        >
          <Icon name={node.badge.icon} class="size-[58%]" stroke={2.4} />
        </span>
      {/if}
    </div>
    <div
      class={[
        'absolute -translate-x-1/2 rv-focus text-center [--rv-blur:5px] [--rv-y:8px]',
        node.captionDelay,
        shape.captionWidth,
        shape.captions[index],
      ]}
    >
      <p class={['font-medium text-ink', shape.captionTitle]}>{translate(node.title)}</p>
      <p class={['mt-1 text-pretty text-ink-soft', shape.captionText]}>{translate(node.text)}</p>
    </div>
  {/each}
</div>
