<script lang="ts">
  import { getTranslator } from '../../../../shared/language/translator.svelte.ts';
  import Icon from '../../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../../shared/ui/section/SectionIntro.svelte';
  import HowDiagram from './HowDiagram.svelte';
  import { LINK_PARTS } from './link-parts.ts';

  const { translate } = getTranslator();
  let selected = $state(LINK_PARTS.length - 1);
</script>

<span id="jak" class="block" aria-hidden="true"></span>

<section id="how-it-works" class="px-4 pt-14 pb-28 sm:px-6 md:pt-16 md:pb-40" aria-labelledby="how-it-works-title">
  <div class="mx-auto max-w-page">
    <div class="mx-auto max-w-[640px] text-center" {@attach reveal}>
      <SectionIntro id="how-it-works-title" eyebrow={translate('how.eyebrow')} title={translate('how.title')} />
      <p
        class="mx-auto mt-4 max-w-[600px] reveal-focus text-center text-lead text-balance text-ink-soft [--reveal-index:2]"
      >
        {translate('how.lead')}
      </p>
    </div>

    <div class="relative mt-16 py-4 md:py-6" {@attach reveal}>
      <div
        class="pointer-events-none absolute inset-0 bg-[radial-gradient(55%_65%_at_50%_45%,oklch(0.56_0.19_258/0.14),transparent_70%)]"
        aria-hidden="true"
      ></div>
      <HowDiagram layout="mobile" />
      <HowDiagram layout="desktop" />
      <p
        class="relative mt-6 flex reveal-focus items-center justify-center gap-2 text-caption text-ink-mute [--reveal-delay:1350ms] md:mt-4 md:text-body-sm"
      >
        <Icon name="trash" class="size-4 shrink-0 text-aegis-300" />
        {translate('how.deleted')}
      </p>
    </div>

    <div
      class="glass mt-6 reveal-rise rounded-card p-5 md:p-8"
      role="group"
      aria-labelledby="how-link-title"
      {@attach reveal}
    >
      <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <div>
          <h3
            id="how-link-title"
            class="reveal-focus font-display text-panel font-light [--reveal-delay:150ms] [--reveal-index:0] md:text-panel-lg"
          >
            {translate('how.link.title')}
          </h3>
          <p class="mt-1 reveal-focus text-body-sm text-ink-soft [--reveal-delay:150ms] [--reveal-index:1]">
            {translate('how.link.sub')}
          </p>
        </div>
      </div>
      <div class="relative mt-6 rounded-pane p-2.5 well md:px-6 md:py-5">
        <div class="flex flex-col gap-2 lg:flex-row lg:gap-6">
          {#each LINK_PARTS as part, index (part.id)}
            {const active = $derived(selected === index)}
            <div
              class={[
                'max-lg:rounded-pane max-lg:p-3.5 max-lg:ring-1 lg:contents',
                active ? 'max-lg:bg-aegis-500/8 max-lg:ring-aegis-400/35' : 'max-lg:bg-white/4 max-lg:ring-white/8',
              ]}
            >
              <button
                class={[
                  'group flex min-w-0 flex-col text-left max-lg:w-full',
                  index === LINK_PARTS.length - 1 ? 'lg:flex-1' : 'lg:shrink-0',
                ]}
                type="button"
                aria-pressed={active}
                onclick={() => (selected = index)}
              >
                <span class="flex w-fit max-w-full flex-col">
                  <span
                    class={[
                      'reveal-focus truncate text-ui whitespace-nowrap tabular-nums transition duration-500 [--reveal-blur:4px] [--reveal-y:6px] lg:text-body',
                      part.tone,
                      part.delays.text,
                      active ? 'opacity-100' : 'opacity-75 group-hover:opacity-100',
                    ]}
                  >
                    <span class="sr-only">{translate(part.title)}: </span>{part.text}
                  </span>
                  <span
                    class={[
                      'mt-2 block h-[3px] w-full origin-left reveal-draw rounded-full',
                      part.bar,
                      part.delays.bar,
                      active && part.glow,
                    ]}
                    aria-hidden="true"
                  ></span>
                </span>
                <span
                  class={[
                    'mt-3 reveal-focus text-body transition-colors duration-500 [--reveal-blur:4px] [--reveal-y:6px]',
                    part.delays.title,
                    active ? 'text-ink' : 'text-ink-soft',
                  ]}>{translate(part.title)}</span
                >
                <span
                  class={[
                    'mt-0.5 reveal-focus text-caption text-ink-mute [--reveal-blur:4px] [--reveal-y:6px]',
                    part.delays.meta,
                  ]}>{translate(part.meta)}</span
                >
              </button>
              {#if active}
                <p class="mt-3 border-t border-white/8 pt-3 text-body-sm text-pretty text-ink-soft lg:hidden">
                  {translate(part.body)}
                </p>
              {/if}
            </div>
          {/each}
        </div>
        <div class="mt-5 stack items-start border-t border-white/8 pt-5 max-lg:hidden lg:items-center">
          {#each LINK_PARTS as part, index (part.id)}
            <p
              class={[
                'text-body text-pretty text-ink-soft transition-opacity duration-500',
                selected === index ? 'opacity-100' : 'pointer-events-none opacity-0',
              ]}
              aria-hidden={selected !== index}
            >
              {translate(part.body)}
            </p>
          {/each}
        </div>
      </div>
    </div>
  </div>
</section>
