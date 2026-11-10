<script lang="ts">
  import Interpolated from '../../../shared/language/Interpolated.svelte';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK, INLINE_TARGET, TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal, REVEAL_INDEXES } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';

  const { translate } = getTranslator();

  const stats = $derived([
    { key: 'about.stat.developer', value: '1', unit: '' },
    {
      key: 'about.stat.price',
      value: translate('about.stat.price.value'),
      unit: translate('about.stat.price.unit'),
    },
    { key: 'about.stat.ads', value: '0', unit: '' },
    { key: 'about.stat.server', value: translate('about.stat.server.value'), unit: '' },
  ] as const);
  const links = [
    { href: REPOSITORY_URL, icon: 'code', key: 'about.code.link' },
    { href: `${REPOSITORY_URL}/issues`, icon: 'message', key: 'about.contact.link' },
  ] as const;
</script>

<section id="about" class="px-4 pt-20 pb-8 sm:px-6 md:pt-28" aria-labelledby="about-title">
  <div class="relative mx-auto max-w-page">
    <div
      class="pointer-events-none absolute top-[58%] left-1/2 h-80 w-full max-w-[64rem] -translate-x-1/2 -translate-y-1/2 reveal-glow rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.16),transparent)]"
      aria-hidden="true"
      {@attach reveal}
    ></div>
    <div class="relative mx-auto max-w-[640px] text-center" {@attach reveal}>
      <SectionIntro id="about-title" eyebrow={translate('about.eyebrow')} title={translate('about.title')} />
      <p
        class="mx-auto mt-5 max-w-[520px] reveal-focus text-lead leading-relaxed text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg"
      >
        {translate('about.text')}
      </p>
    </div>
    <dl class="relative mx-auto mt-12 grid max-w-[60rem] grid-cols-2 md:mt-16 md:grid-cols-4" {@attach reveal}>
      {#each stats as stat, index (stat.key)}
        <div
          class={[
            'flex reveal-focus flex-col items-center gap-3 border-white/8 px-4 py-7 text-center [--reveal-blur:4px] [--reveal-step:110ms] max-md:odd:border-r max-md:nth-[-n+2]:border-b md:py-3 md:not-first:border-l',
            REVEAL_INDEXES[index],
          ]}
        >
          <dt class="order-2 text-caption text-pretty text-ink-soft">{translate(stat.key)}</dt>
          <dd
            class="order-1 font-display text-[3rem] leading-none font-light tracking-[-0.03em] text-aegis-200 tabular-nums md:text-[3.5rem]"
          >
            {stat.value}{#if stat.unit}<span
                class="ml-1.5 text-title [font-weight:400] [letter-spacing:0] text-ink-soft">{stat.unit}</span
              >{/if}
          </dd>
        </div>
      {/each}
    </dl>
    <div
      class="relative mt-10 flex reveal-focus flex-wrap items-center justify-center gap-x-8 gap-y-3 [--reveal-blur:4px] [--reveal-index:4] md:mt-12"
      {@attach reveal}
    >
      {#each links as link (link.key)}
        <a href={link.href} rel="noreferrer" class={[ARROW_LINK, '-my-2.5 min-h-11 text-ui']}>
          <Icon name={link.icon} class="size-4 text-aegis-300/80" />
          {translate(link.key)}
          <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
        </a>
      {/each}
    </div>
    <p
      class="relative mt-12 reveal-focus text-center text-caption text-pretty text-ink-mute [--reveal-blur:4px] md:mt-14"
      {@attach reveal}
    >
      <Interpolated text={translate('about.terms')} placeholder="link"
        ><a href="/terms" class={[TEXT_LINK, INLINE_TARGET]}>{translate('about.terms.link')}</a></Interpolated
      >
    </p>
  </div>
</section>
