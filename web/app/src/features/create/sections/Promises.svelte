<script lang="ts">
  import { formatDays } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import GithubMark from '../../../shared/shell/GithubMark.svelte';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Button from '../../../shared/ui/button/Button.svelte';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import { TEXT_LINK } from '../../../shared/ui/link/link-classes.ts';
  import { reveal } from '../../../shared/ui/reveal/reveal.ts';
  import IconTile from '../../../shared/ui/tile/IconTile.svelte';
  import { TTL_CHOICES } from '../composer/create-form.ts';
  import { PROMISES } from './promises.ts';

  const translator = getTranslator();
  const { translate } = translator;
  const SECONDS_PER_DAY = 86_400;
  const TOMBSTONE_DAYS = 7;

  const longest = $derived(formatDays(Math.max(...TTL_CHOICES) / SECONDS_PER_DAY, translator.language));
  const forgotten = $derived(formatDays(TOMBSTONE_DAYS, translator.language));
  const CARD =
    'glow-on-hover glass rv-rise hover-lift rounded-card p-6 [--rv-s:0.96] [--rv-step:120ms] [--rv-time:1.1s]';
</script>

<span id="sliby" class="block" aria-hidden="true"></span>

<section id="promises" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="promise-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="max-w-[640px]" {@attach reveal}>
      <p class="rv-focus text-eyebrow text-aegis-300 uppercase [--rv-i:0]">{translate('promise.eyebrow')}</p>
      <h2
        id="promise-title"
        class="mt-3 rv-focus font-display text-section font-light text-balance [--rv-i:1] md:text-section-lg"
      >
        {translate('promise.title')}
      </h2>
    </div>
    <div class="mt-14 grid gap-4 md:grid-cols-6 md:gap-5">
      {#each PROMISES as promise (promise.key)}
        <article class={[CARD, promise.layout, promise.motion]} {@attach reveal}>
          <IconTile
            icon={promise.icon}
            tone="current"
            class={['rv-pop [--rv-delay:320ms] [--rv-step:120ms]', promise.order]}
          />
          <h3
            class={promise.wide
              ? 'mt-6 font-display text-panel font-light md:text-panel-lg'
              : 'mt-5 text-title font-medium'}
          >
            {translate(`${promise.key}.title`, { days: longest })}
          </h3>
          <p
            class={promise.wide
              ? 'mt-2 max-w-[480px] leading-relaxed text-pretty text-ink-soft'
              : 'mt-2 text-body text-pretty text-ink-soft'}
          >
            {translate(`${promise.key}.text`, { days: forgotten })}
          </p>
          <a
            data-verify
            href={promise.verify}
            class={[TEXT_LINK, 'mt-3 -mb-2.5 inline-flex min-h-11 items-center gap-1.5 text-caption']}
            >{translate('promise.verify', { target: translate(`${promise.key}.verify`) })}</a
          >
        </article>
      {/each}
      <article
        class={[
          CARD,
          'flex flex-col gap-6 md:col-span-6 md:flex-row md:items-center md:justify-between md:p-8 md:[--rv-i:0] md:[--rv-x:0px] md:[--rv-y:28px]',
        ]}
        {@attach reveal}
      >
        <div class="flex items-center gap-5 max-md:flex-col max-md:items-start max-md:gap-6">
          <IconTile icon="code" tone="current" class="rv-pop [--rv-delay:320ms] [--rv-step:120ms] md:[--rv-i:0]" />
          <div>
            <h3 class="font-display text-panel font-light md:text-panel-lg">{translate('promise.open.title')}</h3>
            <p class="mt-2 max-w-[640px] leading-relaxed text-pretty text-ink-soft">{translate('promise.open.text')}</p>
          </div>
        </div>
        <Button variant="ghost" href={REPOSITORY_URL} class="self-start md:self-auto">
          <GithubMark class="size-4 shrink-0 max-md:hidden" animated />
          {translate('promise.open.link')}
          <Icon name="external" class="size-4 md:size-3.5 md:[stroke-width:2] md:opacity-70" animated />
        </Button>
      </article>
    </div>
  </div>
</section>
