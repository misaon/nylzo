<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';

  const { translate } = getTranslator();

  const PLATE_TILT = '[transform:rotateX(60deg)_rotateZ(-45deg)]';
  const layers = [
    {
      key: 'tech.crypto',
      icon: 'lock',
      face: 'tile-current text-white',
      depth: 'z-40 [--reveal-index:0]',
    },
    {
      key: 'tech.web',
      icon: 'globe',
      face: 'bg-aegis-500/30 text-aegis-200 ring-1 ring-aegis-300/35 shadow-[inset_0_1px_0_oklch(1_0_0/0.18)]',
      depth: 'z-30 [--reveal-index:1]',
    },
    {
      key: 'tech.server',
      icon: 'server',
      face: 'bg-aegis-500/18 text-aegis-200 ring-1 ring-aegis-300/25 shadow-[inset_0_1px_0_oklch(1_0_0/0.14)]',
      depth: 'z-20 [--reveal-index:2]',
    },
    {
      key: 'tech.release',
      icon: 'shieldCheck',
      face: 'bg-white/7 text-ink-soft ring-1 ring-white/14 shadow-[inset_0_1px_0_oklch(1_0_0/0.12)]',
      depth: 'z-10 [--reveal-index:3]',
    },
  ] as const;
  const tests = [
    { grade: 'A+', name: 'securityheaders.com' },
    { grade: 'A+', name: 'Mozilla Observatory' },
    { grade: '100', name: 'PageSpeed Insights' },
    { grade: undefined, name: 'OpenSSF Scorecard' },
  ] as const;
</script>

<section id="technology" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="tech-title">
  <div class="mx-auto grid max-w-page gap-x-16 gap-y-12 lg:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)] lg:gap-y-10">
    <div class="lg:self-end" {@attach reveal}>
      <SectionIntro id="tech-title" eyebrow={translate('tech.eyebrow')} title={translate('tech.title')} />
      <p
        class="mt-5 max-w-[480px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('tech.sub')}
      </p>
    </div>

    <div class="relative lg:col-start-2 lg:row-span-2 lg:row-start-1" {@attach reveal}>
      <div
        class="pointer-events-none absolute top-1/2 left-[4.5rem] size-80 -translate-x-1/2 -translate-y-1/2 reveal-glow rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.22),transparent)] sm:left-32 sm:size-[28rem]"
        aria-hidden="true"
      ></div>
      <span
        class="pointer-events-none absolute top-14 bottom-14 left-[4.5rem] w-px -translate-x-1/2 bg-linear-to-b from-aegis-300/0 via-aegis-300/50 to-aegis-300/0 sm:left-32"
        aria-hidden="true"
      ></span>
      <ol role="list" class="relative isolate pt-6 pb-10 sm:pt-8 sm:pb-12">
        {#each layers as layer (layer.key)}
          <li
            class={[
              'relative grid reveal-focus grid-cols-[9rem_minmax(0,1fr)] items-center [--reveal-blur:4px] [--reveal-step:110ms] sm:grid-cols-[16rem_minmax(0,1fr)]',
              layer.depth,
            ]}
          >
            <div class="relative h-[4.75rem] sm:h-[5.25rem]" aria-hidden="true">
              <div class="absolute top-1/2 left-1/2 size-24 -translate-x-1/2 -translate-y-1/2 sm:size-44">
                <div class="relative size-full motion-safe:animate-layer-float">
                  <span
                    class={[
                      'absolute inset-0 translate-y-1.5 rounded-[1.75rem] bg-void-900 ring-1 ring-white/10 sm:translate-y-2.5',
                      PLATE_TILT,
                    ]}
                  ></span>
                  <span class={['absolute inset-0 grid place-items-center rounded-[1.75rem]', PLATE_TILT, layer.face]}>
                    <Icon name={layer.icon} class="size-10 scale-y-[1.4] rotate-45 sm:size-16" stroke={1.3} />
                  </span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-3 sm:gap-4">
              <span
                class="h-px w-5 shrink-0 bg-linear-to-r from-aegis-300/70 to-aegis-300/10 sm:w-10"
                aria-hidden="true"
              ></span>
              <div class="min-w-0">
                <h3 class="text-title-sm text-balance wrap-anywhere hyphens-auto text-ink">
                  {translate(`${layer.key}.title`)}
                </h3>
                <p class="mt-0.5 text-caption text-pretty text-ink-soft">{translate(`${layer.key}.text`)}</p>
              </div>
            </div>
          </li>
        {/each}
      </ol>
    </div>

    <div
      class="reveal-focus [--reveal-blur:4px] [--reveal-index:3] lg:col-start-1 lg:row-start-2 lg:self-start"
      {@attach reveal}
    >
      <p class="text-caption text-ink-soft">{translate('tech.tests.title')}</p>
      <ul role="list" class="mt-2 grid grid-cols-2 overflow-hidden rounded-pane well">
        {#each tests as test (test.name)}
          <li class="flex flex-col gap-2 border-white/8 px-5 py-4 odd:border-r nth-[-n+2]:border-b">
            {#if test.grade}
              <span
                class="font-display text-[1.75rem] leading-none font-light tracking-[-0.03em] text-aegis-200 tabular-nums"
                >{test.grade}</span
              >
            {:else}
              <span class="grid h-7 items-center text-aegis-200">
                <Icon name="gauge" class="size-7" stroke={1.5} />
              </span>
            {/if}
            <span class="text-micro wrap-anywhere text-ink-soft">{test.name}</span>
          </li>
        {/each}
      </ul>
    </div>
  </div>
</section>
