<script lang="ts">
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK, INLINE_TARGET, TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const facts = [
    { key: 'about.running', icon: 'heart', order: '[--rv-i:0] sm:[--rv-i:0]' },
    { key: 'about.server', icon: 'server', order: '[--rv-i:1] sm:[--rv-i:1]' },
    { key: 'about.contact', icon: 'message', order: '[--rv-i:2] sm:[--rv-i:1]', href: `${REPOSITORY_URL}/issues` },
    { key: 'about.code', icon: 'code', order: '[--rv-i:3] sm:[--rv-i:2]', href: REPOSITORY_URL },
  ] as const;
</script>

<section id="about" class="px-4 pt-20 pb-8 sm:px-6 md:pt-28" aria-labelledby="about-title">
  <div class="mx-auto grid max-w-page gap-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] md:gap-10">
    <div {@attach reveal}>
      <SectionIntro id="about-title" eyebrow={translate('about.eyebrow')} title={translate('about.title')} />
      <p class="mt-5 rv-focus text-lead text-pretty text-ink-soft [--rv-i:2] md:text-lead-lg">
        {translate('about.text')}
      </p>
      <p class="mt-4 rv-focus text-body-sm text-pretty text-ink-mute [--rv-i:3]">{translate('about.tools')}</p>
    </div>
    <dl class="grid gap-3 sm:grid-cols-2" {@attach reveal}>
      {#each facts as fact (fact.key)}
        <div
          class={[
            'glass-soft flex rv-rise flex-col rounded-card p-5 [--rv-delay:150ms] [--rv-step:100ms] [--rv-y:16px] md:p-6',
            fact.order,
          ]}
        >
          <dt class="flex items-center gap-3 text-title-sm text-ink">
            <IconTile
              icon={fact.icon}
              tone="neutral"
              size="medium"
              class={['rv-pop [--rv-delay:400ms] [--rv-step:100ms]', fact.order]}
            />
            {translate(`${fact.key}.title`)}
          </dt>
          <dd class="mt-3 text-body-sm text-pretty text-ink-soft">{translate(`${fact.key}.text`)}</dd>
          {#if 'href' in fact}
            <dd class="mt-auto pt-2">
              <a href={fact.href} rel="noreferrer" class={[ARROW_LINK, '-mb-2.5 min-h-11 text-caption']}>
                {translate(`${fact.key}.link`)}
                <Icon name="arrowRight" class="size-3.5" stroke={2} animated />
              </a>
            </dd>
          {/if}
        </div>
      {/each}
    </dl>
    <p class="rv-focus text-caption text-pretty text-ink-mute [--rv-blur:4px] md:col-span-2" {@attach reveal}>
      {translate('about.terms')}
      <a href="/terms" class={[TEXT_LINK, INLINE_TARGET]}>{translate('about.terms.link')}</a>.
    </p>
  </div>
</section>
