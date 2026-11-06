<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const extras = [
    { key: 'extra.stolen', icon: 'search', order: 'md:[--rv-i:0] lg:[--rv-i:0]' },
    { key: 'extra.signed', icon: 'key', order: 'md:[--rv-i:1] lg:[--rv-i:1]' },
    { key: 'extra.padding', icon: 'file', order: 'md:[--rv-i:0] lg:[--rv-i:2]' },
    { key: 'extra.forget', icon: 'trash', order: 'md:[--rv-i:1] lg:[--rv-i:0]' },
    { key: 'extra.twice', icon: 'code', order: 'md:[--rv-i:0] lg:[--rv-i:1]' },
    { key: 'extra.standards', icon: 'lock', order: 'md:[--rv-i:1] lg:[--rv-i:2]' },
  ] as const;
</script>

<span id="algoritmy" class="block" aria-hidden="true"></span>

<section id="algorithms" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="extra-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="max-w-[640px]" {@attach reveal}>
      <p class="rv-focus text-eyebrow text-aegis-300 uppercase [--rv-i:0]">{translate('extra.eyebrow')}</p>
      <h2
        id="extra-title"
        class="mt-3 rv-focus font-display text-section font-light text-balance [--rv-i:1] md:text-section-lg"
      >
        {translate('extra.title')}
      </h2>
      <p
        class="mt-5 max-w-[520px] rv-focus text-lead leading-relaxed text-pretty text-ink-soft [--rv-i:2] md:text-lead-lg"
      >
        {translate('extra.sub')}
      </p>
    </div>
    <div class="mt-14 grid gap-4 md:grid-cols-2 md:gap-5 lg:grid-cols-3">
      {#each extras as extra (extra.key)}
        <article
          class={[
            'glow-on-hover glass flex hover-lift rv-rise flex-col rounded-card p-6 [--rv-step:120ms] md:p-7',
            extra.order,
          ]}
          {@attach reveal}
        >
          <div class="flex items-center gap-4">
            <IconTile
              icon={extra.icon}
              tone="current"
              class={['rv-pop [--rv-delay:300ms] [--rv-step:120ms]', extra.order]}
            />
            <h3 class="text-title font-medium text-balance">{translate(`${extra.key}.title`)}</h3>
          </div>
          <p class="mt-4 text-body text-pretty text-ink-soft">{translate(`${extra.key}.text`)}</p>
          <p class="mt-auto pt-5">
            <span
              class="inline-flex items-center gap-2 rounded-full bg-aegis-500/12 px-3 py-1.5 text-caption text-aegis-200 tabular-nums ring-1 ring-aegis-400/35"
            >
              <Icon
                name="check"
                class={['size-3.5 shrink-0 rv-check [--rv-delay:700ms] [--rv-step:120ms]', extra.order]}
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
