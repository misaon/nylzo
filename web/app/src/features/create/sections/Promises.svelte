<script lang="ts">
  import { formatDays } from '../../../shared/language/format.ts';
  import { getTranslator } from '../../../shared/language/translator.svelte.ts';
  import { RETENTION_DAYS } from '../../../shared/sent-records/sent-record.ts';
  import { REPOSITORY_URL } from '../../../shared/shell/repository.ts';
  import Icon from '../../../shared/ui/Icon.svelte';
  import { TTL_CHOICES } from '../composer/create-form.ts';

  const translator = getTranslator();
  const { translate } = translator;
  const SECONDS_PER_DAY = 86_400;

  const longest = $derived(formatDays(Math.max(...TTL_CHOICES) / SECONDS_PER_DAY, translator.language));
  const forgotten = $derived(formatDays(RETENTION_DAYS, translator.language));

  const small = [
    { key: 'promise.preview', icon: 'eye', reveal: 'reveal-up reveal-step-1' },
    { key: 'promise.logs', icon: 'server', reveal: 'reveal-up' },
    { key: 'promise.ttl', icon: 'clock', reveal: 'reveal-up reveal-step-1' },
    { key: 'promise.cancel', icon: 'refresh', reveal: 'reveal-up reveal-step-2' },
  ] as const;
</script>

<section id="sliby" class="px-4 py-28 sm:px-6 md:py-40" aria-labelledby="promise-title">
  <div class="mx-auto max-w-[1160px]">
    <div class="max-w-[640px] reveal-up">
      <p class="text-sm font-medium text-aegis-300">{translate('promise.eyebrow')}</p>
      <h2
        id="promise-title"
        class="mt-3 font-display text-[28px] leading-[1.2] font-light tracking-[-0.02em] text-balance md:text-[40px]"
      >
        {translate('promise.title')}
      </h2>
    </div>
    <div class="mt-14 grid gap-4 md:grid-cols-6 md:gap-5">
      <article class="glow-on-hover glass hover-lift reveal-zoom rounded-card p-6 md:col-span-4 md:p-8">
        <span
          class="grid size-12 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-300 ring-1 ring-aegis-400/30"
          aria-hidden="true"
        >
          <Icon name="userOff" stroke={1.5} class="size-6" />
        </span>
        <h3 class="mt-6 font-display text-xl font-light tracking-[-0.015em] md:text-2xl">
          {translate('promise.account.title')}
        </h3>
        <p class="mt-2 max-w-[480px] leading-relaxed text-pretty text-ink-soft">{translate('promise.account.text')}</p>
      </article>
      {#each small as item (item.key)}
        <article class={['glow-on-hover glass hover-lift rounded-card p-6 md:col-span-2 md:p-7', item.reveal]}>
          <span
            class="grid size-11 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-300 ring-1 ring-aegis-400/30"
            aria-hidden="true"
          >
            <Icon name={item.icon} stroke={1.5} class="size-5" />
          </span>
          <h3 class="mt-5 text-lg font-medium">{translate(`${item.key}.title`, { days: longest })}</h3>
          <p class="mt-2 text-[15px] leading-relaxed text-pretty text-ink-soft">
            {translate(`${item.key}.text`, { days: forgotten })}
          </p>
        </article>
      {/each}
      <article
        class="glow-on-hover glass flex hover-lift reveal-tilt flex-col gap-6 rounded-card p-6 md:col-span-6 md:flex-row md:items-center md:justify-between md:p-8"
      >
        <div class="flex items-start gap-5">
          <span
            class="grid size-12 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-300 ring-1 ring-aegis-400/30"
            aria-hidden="true"
          >
            <Icon name="code" stroke={1.5} class="size-6" />
          </span>
          <div>
            <h3 class="font-display text-xl font-light tracking-[-0.015em]">{translate('promise.open.title')}</h3>
            <p class="mt-2 max-w-[640px] leading-relaxed text-pretty text-ink-soft">{translate('promise.open.text')}</p>
          </div>
        </div>
        <a
          href={REPOSITORY_URL}
          class="btn-ghost flex h-11 shrink-0 hover-lift items-center justify-center gap-2 self-start rounded-control px-5 text-sm md:self-auto"
        >
          {translate('promise.open.link')}
          <Icon name="arrowRight" class="size-4" />
        </a>
      </article>
    </div>
  </div>
</section>
