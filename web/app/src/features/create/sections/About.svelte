<script lang="ts">
  import Interpolated from '../../../shared/language/Interpolated.svelte';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { ARROW_LINK, INLINE_TARGET, TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import SectionIntro from '../../../shared/ui/section/SectionIntro.svelte';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';

  const { translate } = getTranslator();

  const facts = [
    { key: 'about.running', icon: 'heart', revealIndex: '[--reveal-index:0] sm:[--reveal-index:0]' },
    { key: 'about.server', icon: 'server', revealIndex: '[--reveal-index:1] sm:[--reveal-index:1]' },
    {
      key: 'about.contact',
      icon: 'message',
      revealIndex: '[--reveal-index:2] sm:[--reveal-index:1]',
      href: `${REPOSITORY_URL}/issues`,
    },
    { key: 'about.code', icon: 'code', revealIndex: '[--reveal-index:3] sm:[--reveal-index:2]', href: REPOSITORY_URL },
  ] as const;
</script>

<section id="about" class="px-4 pt-20 pb-8 sm:px-6 md:pt-28" aria-labelledby="about-title">
  <div class="mx-auto grid max-w-page gap-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] md:gap-10">
    <div {@attach reveal}>
      <SectionIntro id="about-title" eyebrow={translate('about.eyebrow')} title={translate('about.title')} />
      <p class="mt-5 reveal-focus text-lead text-pretty text-ink-soft [--reveal-index:2] md:text-lead-lg">
        {translate('about.text')}
      </p>
      <p class="mt-4 reveal-focus text-body-sm text-pretty text-ink-mute [--reveal-index:3]">
        {translate('about.tools')}
      </p>
    </div>
    <dl class="grid gap-3 sm:grid-cols-2" {@attach reveal}>
      {#each facts as fact (fact.key)}
        <div
          class={[
            'glass-soft flex reveal-rise flex-col rounded-card p-5 [--reveal-delay:150ms] [--reveal-step:100ms] [--reveal-y:16px] md:p-6',
            fact.revealIndex,
          ]}
        >
          <dt class="flex items-center gap-3 text-title-sm text-ink">
            <IconTile
              icon={fact.icon}
              tone="neutral"
              size="medium"
              class={['reveal-pop [--reveal-delay:400ms] [--reveal-step:100ms]', fact.revealIndex]}
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
    <p class="reveal-focus text-caption text-pretty text-ink-mute [--reveal-blur:4px] md:col-span-2" {@attach reveal}>
      <Interpolated text={translate('about.terms')} placeholder="link"
        ><a href="/terms" class={[TEXT_LINK, INLINE_TARGET]}>{translate('about.terms.link')}</a></Interpolated
      >
    </p>
  </div>
</section>
