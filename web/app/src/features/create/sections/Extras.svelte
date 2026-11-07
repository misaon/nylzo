<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const extras = [
    { key: 'extra.stolen', icon: 'search', revealIndex: 'md:[--reveal-index:0] lg:[--reveal-index:0]' },
    { key: 'extra.signed', icon: 'key', revealIndex: 'md:[--reveal-index:1] lg:[--reveal-index:1]' },
    { key: 'extra.padding', icon: 'file', revealIndex: 'md:[--reveal-index:0] lg:[--reveal-index:2]' },
    { key: 'extra.forget', icon: 'trash', revealIndex: 'md:[--reveal-index:1] lg:[--reveal-index:0]' },
    { key: 'extra.twice', icon: 'code', revealIndex: 'md:[--reveal-index:0] lg:[--reveal-index:1]' },
    { key: 'extra.standards', icon: 'lock', revealIndex: 'md:[--reveal-index:1] lg:[--reveal-index:2]' },
  ] as const;
</script>

<span id="algoritmy" class="block" aria-hidden="true"></span>

<section id="algorithms" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="extra-title">
  <div class="mx-auto max-w-page">
    <div class="max-w-[640px]" {@attach reveal}>
      <SectionIntro id="extra-title" eyebrow={translate('extra.eyebrow')} title={translate('extra.title')} />
      <p
        class="mt-5 max-w-[520px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('extra.sub')}
      </p>
    </div>
    <div class="mt-14 grid gap-4 md:grid-cols-2 md:gap-5 lg:grid-cols-3">
      {#each extras as extra (extra.key)}
        <article
          class={[
            'glow-on-hover glass flex hover-lift reveal-rise flex-col rounded-card p-6 [--reveal-step:120ms] md:p-7',
            extra.revealIndex,
          ]}
          {@attach reveal}
        >
          <div class="flex items-center gap-4">
            <IconTile
              icon={extra.icon}
              tone="current"
              class={['reveal-pop [--reveal-delay:300ms] [--reveal-step:120ms]', extra.revealIndex]}
            />
            <h3 class="text-title text-balance">{translate(`${extra.key}.title`)}</h3>
          </div>
          <p class="mt-4 text-body text-pretty text-ink-soft">{translate(`${extra.key}.text`)}</p>
          <p class="mt-auto pt-5">
            <span
              class="inline-flex items-center gap-2 rounded-full bg-aegis-500/12 px-3 py-1.5 text-caption text-aegis-200 tabular-nums ring-1 ring-aegis-400/35"
            >
              <Icon
                name="check"
                class={[
                  'size-3.5 shrink-0 reveal-check [--reveal-delay:700ms] [--reveal-step:120ms]',
                  extra.revealIndex,
                ]}
                stroke={2}
              />
              {translate(`${extra.key}.proof`)}
            </span>
          </p>
        </article>
      {/each}
    </div>
  </div>
</section>
