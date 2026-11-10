<script lang="ts">
  import { tick } from 'svelte';

  import { createApi } from '../../shared/api/api.ts';
  import type { Transparency } from '../../shared/api/api.ts';
  import { BUILD_INFO } from '../../shared/build-info.ts';
  import type { MessageKey } from '../../shared/language/catalog.ts';
  import { formatBytes, formatDuration } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { instanceOrigin, OFFICIAL_ORIGIN } from '../../shared/local-client/instance-origin.ts';
  import GithubMark from '../../shared/shell/GithubMark.svelte';
  import { REPOSITORY_URL, VERIFIED_CLIENTS_URL } from '../../shared/shell/repository.ts';
  import { SECONDS_PER_DAY } from '../../shared/ttl/ttl-choices.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import CopyButton from '../../shared/ui/button/CopyButton.svelte';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../shared/ui/icon/icons.ts';
  import { INLINE_TARGET, TEXT_LINK } from '../../shared/ui/link/link-classes.ts';
  import StateCard from '../../shared/ui/notice/StateCard.svelte';
  import IconTile from '../../shared/ui/tile/IconTile.svelte';
  import { contactHref, contactLabel } from './transparency.ts';

  type View =
    | { readonly view: 'loading' }
    | { readonly view: 'failed' }
    | { readonly view: 'ready'; readonly data: Transparency };

  interface Entry {
    readonly title: string;
    readonly text: string;
  }

  interface Fact {
    readonly key: MessageKey;
    readonly value: string;
  }

  const CARD = 'min-w-0 rounded-pane bg-white/4 shadow-rim ring-1 ring-white/8';
  const CHIP =
    'flex items-center gap-1.5 rounded-full bg-white/4 py-1 pr-3 pl-2 text-caption text-ink-soft ring-1 ring-white/12';

  const SECTIONS = [
    { id: 'transparency-operator', nav: 'transparency.operator.title' },
    { id: 'transparency-stores', nav: 'transparency.stores.title' },
    { id: 'transparency-logs', nav: 'transparency.logs.title' },
    { id: 'transparency-checks', nav: 'transparency.checks.title' },
    { id: 'transparency-others', nav: 'transparency.others.title' },
    { id: 'transparency-personal', nav: 'transparency.personal.title' },
    { id: 'transparency-limits', nav: 'transparency.limits.short' },
    { id: 'transparency-verify', nav: 'transparency.verify.short' },
  ] as const satisfies readonly { id: string; nav: MessageKey }[];

  const WRITTEN = [
    'transparency.logs.written.time',
    'transparency.logs.written.method',
    'transparency.logs.written.route',
    'transparency.logs.written.status',
    'transparency.logs.written.count',
  ] as const;
  const NEVER_WRITTEN = [
    'transparency.logs.never.id',
    'transparency.logs.never.address',
    'transparency.logs.never.keys',
    'transparency.logs.never.signatures',
    'transparency.logs.never.bodies',
    'transparency.logs.never.size',
    'transparency.logs.never.duration',
  ] as const;
  const SUMMARY = [
    'transparency.summary.content',
    'transparency.summary.addresses',
    'transparency.summary.ended',
  ] as const;
  const official = new URL(instanceOrigin()).origin === OFFICIAL_ORIGIN;
  const CHECKS = official
    ? (['changes', 'ai', 'watch', 'ratings', 'audit'] as const)
    : (['changes', 'ai', 'ratings', 'audit'] as const);
  const PERSONAL = official
    ? (['controller', 'basis', 'processor', 'browser'] as const)
    : (['controller', 'basis', 'browser'] as const);

  const translator = getTranslator();
  const { translate } = translator;
  const api = createApi();
  const command = `nylzo verify-client --server ${instanceOrigin()}`;

  let view = $state.raw<View>({ view: 'loading' });
  const data = $derived(view.view === 'ready' ? view.data : undefined);

  const days = (seconds: number) => translator.plural('count.day', Math.round(seconds / SECONDS_PER_DAY));
  const period = (seconds: number) =>
    seconds % SECONDS_PER_DAY === 0 ? days(seconds) : formatDuration(seconds, translator.language);
  const limit = (value: number) => (value === 0 ? translate('transparency.limits.none') : String(value));

  function stored({ retention }: Transparency): readonly Entry[] {
    const entry = (
      name: 'content' | 'metadata' | 'keys' | 'ended' | 'enforced' | 'reports' | 'counters' | 'addresses',
      text: string,
    ) => ({ title: translate(`transparency.stores.${name}.title`), text });
    return [
      entry('content', translate('transparency.stores.content')),
      entry('metadata', translate('transparency.stores.metadata')),
      entry('keys', translate('transparency.stores.keys')),
      entry('ended', translate('transparency.stores.ended', { days: days(retention.tombstoneSeconds) })),
      entry('enforced', translate('transparency.stores.enforced')),
      ...(retention.abuseReportSeconds === undefined
        ? []
        : [entry('reports', translate('transparency.stores.reports', { days: days(retention.abuseReportSeconds) }))]),
      entry('counters', translate('transparency.stores.counters', { days: days(retention.dailyCounterSeconds) })),
      entry('addresses', translate('transparency.stores.addresses')),
    ];
  }

  function stats({ limits }: Transparency): readonly Fact[] {
    return [
      { key: 'transparency.limits.file', value: formatBytes(limits.maxFileBytes, translator.language) },
      { key: 'transparency.limits.ttl', value: period(limits.maxTtlSeconds) },
      { key: 'transparency.limits.opens', value: String(limits.maxOpens) },
    ];
  }

  function rates({ rates: per }: Transparency): readonly Fact[] {
    return [
      {
        key: 'transparency.limits.create',
        value: translate('transparency.limits.create.value', {
          hour: limit(per.createPerHour),
          day: limit(per.createPerDay),
        }),
      },
      {
        key: 'transparency.limits.read',
        value: translate('transparency.limits.read.value', { hour: limit(per.readPerHour) }),
      },
      {
        key: 'transparency.limits.global',
        value:
          per.globalDailyCreates === 0
            ? translate('transparency.limits.none')
            : translate('transparency.limits.global.value', { max: per.globalDailyCreates }),
      },
    ];
  }

  // The sections arrive after the page loads, so a link to one of them scrolls only once it exists.
  async function load() {
    view = { view: 'loading' };
    try {
      view = { view: 'ready', data: await api.transparency() };
    } catch {
      view = { view: 'failed' };
      return;
    }
    await tick();
    const section = decodeURIComponent(location.hash.slice(1));
    if (section !== '') {
      document.querySelector(`#${CSS.escape(section)}`)?.scrollIntoView({ block: 'start' });
    }
  }

  void load();
</script>

<svelte:head>
  <title>{translate('title.transparency')}</title>
</svelte:head>

{#snippet heading(id: string, icon: IconName, title: string)}
  <div class="flex items-center gap-3">
    <IconTile {icon} tone="current" size="medium" />
    <h2 {id} class="text-title text-ink">{title}</h2>
  </div>
{/snippet}

{#snippet entries(list: readonly Entry[])}
  <ul role="list" class="mt-6">
    {#each list as entry (entry.title)}
      <li class="border-t border-white/8 py-4 first:border-t-0 first:pt-0 last:pb-0">
        <h3 class="text-title-sm text-ink">{entry.title}</h3>
        <p class="mt-1 text-body-sm text-pretty text-ink-soft">{entry.text}</p>
      </li>
    {/each}
  </ul>
{/snippet}

{#snippet caution(title: string, text: string)}
  <div class="mt-6 flex gap-3.5 rounded-pane bg-solar-300/[0.08] p-4 ring-1 ring-solar-300/30 sm:p-5">
    <IconTile icon="caution" tone="warning" size="small" />
    <div class="min-w-0">
      <h3 class="text-title-sm text-ink">{title}</h3>
      <p class="mt-1 text-body-sm text-pretty text-ink-soft">{text}</p>
    </div>
  </div>
{/snippet}

{#snippet chips(title: string, list: readonly MessageKey[], icon: IconName, tone: string)}
  <h3 class="text-title-sm text-ink">{title}</h3>
  <ul role="list" class="mt-3 flex flex-wrap gap-1.5">
    {#each list as chip (chip)}
      <li class={CHIP}>
        <Icon name={icon} class={['size-3.5 shrink-0', tone]} stroke={2} />
        {translate(chip)}
      </li>
    {/each}
  </ul>
{/snippet}

{#snippet detail(term: MessageKey, value: string, href?: string)}
  <div class={[CARD, 'px-4 py-3']}>
    <dt class="text-micro text-ink-mute">{translate(term)}</dt>
    <dd class="mt-1 text-body wrap-anywhere text-ink">
      {#if href}
        <a {href} rel="noreferrer" class={[TEXT_LINK, INLINE_TARGET]}>{contactLabel(value)}</a>
      {:else}
        {value}
      {/if}
    </dd>
  </div>
{/snippet}

{#snippet fact(term: string, value: string, testId: string, wide = false)}
  <div class={[CARD, 'px-4 py-3', wide && 'sm:col-span-2']}>
    <dt class="text-micro text-ink-mute">{term}</dt>
    <dd data-testid={testId} class="mt-1 text-body wrap-anywhere text-aegis-200 tabular-nums">{value}</dd>
  </div>
{/snippet}

<section aria-labelledby="transparency-title" class="grid gap-8">
  <div>
    <h1 id="transparency-title" class="font-display text-page font-light text-ink md:text-page-lg">
      {translate('transparency.title')}
    </h1>
    <p class="mt-2 max-w-[560px] text-body text-pretty text-ink-soft">{translate('transparency.intro')}</p>
    {#if data}
      <ul
        role="list"
        class="mt-5 grid animate-appear gap-2.5 rounded-pane bg-aegis-500/6 p-4 text-body-sm text-ink ring-1 ring-aegis-400/20 sm:p-5"
      >
        {#each SUMMARY as item (item)}
          <li class="flex items-start gap-2.5">
            <Icon name="check" class="mt-0.5 size-4 shrink-0 text-aegis-300" stroke={2} />
            <span>{translate(item, { days: days(data.retention.tombstoneSeconds) })}</span>
          </li>
        {/each}
      </ul>
      <p class="mt-3 flex max-w-[600px] animate-appear items-center gap-2 text-caption text-pretty text-ink-mute">
        <Icon name="info" class="size-4 shrink-0" />
        <span>{translate('transparency.configured')}</span>
      </p>
      <nav aria-label={translate('transparency.contents')} class="mt-6 animate-appear">
        <ul role="list" class="flex flex-wrap gap-2">
          {#each SECTIONS as section (section.id)}
            <li>
              <a
                href={`#${section.id}`}
                class="flex h-11 items-center rounded-full bg-white/4 px-3.5 text-caption text-ink-soft ring-1 ring-white/12 transition hover:bg-white/10 hover:text-ink md:h-9"
              >
                {translate(section.nav)}
              </a>
            </li>
          {/each}
        </ul>
      </nav>
    {/if}
  </div>

  {#if view.view === 'loading'}
    <StateCard loading={translate('transparency.loading')} />
  {:else if view.view === 'failed'}
    <StateCard>
      <IconTile icon="alert" tone="warning" class="size-14 rounded-full" iconClass="size-6" stroke={1.6} />
      <p role="alert" class="mt-5 text-title-sm text-ink">{translate('transparency.failed')}</p>
      <Button variant="primary" class="mt-6" onclick={() => void load()}>
        <Icon name="refresh" class="size-4 shrink-0" animated />
        <span class="relative">{translate('action.retry')}</span>
      </Button>
    </StateCard>
  {:else if data}
    {const operator = $derived(data.operator)}
    <section aria-labelledby="transparency-operator" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-operator', 'rack', translate('transparency.operator.title'))}
      {#if operator && (operator.name || operator.contact || operator.hosting)}
        <dl class="mt-5 grid gap-3 sm:grid-cols-2">
          {#if operator.name}
            {@render detail('transparency.operator.name', operator.name)}
          {/if}
          {#if operator.contact}
            {@render detail('transparency.operator.contact', operator.contact, contactHref(operator.contact))}
          {/if}
          {#if operator.hosting}
            {@render detail('transparency.operator.hosting', operator.hosting)}
          {/if}
        </dl>
      {:else}
        <p class="mt-5 text-body-sm text-ink-soft">{translate('transparency.operator.missing')}</p>
      {/if}
    </section>

    <section aria-labelledby="transparency-stores" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-stores', 'storage', translate('transparency.stores.title'))}
      {@render entries(stored(data))}
    </section>

    <section aria-labelledby="transparency-logs" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-logs', 'list', translate('transparency.logs.title'))}
      <div class="mt-6 grid gap-3 sm:grid-cols-2">
        <div class={[CARD, 'p-4 sm:p-5']}>
          {@render chips(translate('transparency.logs.written'), WRITTEN, 'check', 'text-aegis-300')}
          <p class="mt-3 text-caption text-pretty text-ink-mute">{translate('transparency.logs.written.note')}</p>
        </div>
        <div class={[CARD, 'p-4 sm:p-5']}>
          {@render chips(translate('transparency.logs.never'), NEVER_WRITTEN, 'cross', 'text-ink-mute')}
        </div>
      </div>
    </section>

    <section aria-labelledby="transparency-checks" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-checks', 'list', translate('transparency.checks.title'))}
      <p class="mt-4 max-w-[720px] text-body-sm text-pretty text-ink-soft">{translate('transparency.checks.note')}</p>
      <dl class="mt-4">
        {#each CHECKS as check (check)}
          <div class="flex flex-col gap-1 border-t border-white/8 py-3 sm:flex-row sm:gap-6">
            <dt class="w-56 shrink-0 text-body-sm font-medium text-ink">
              {translate(`transparency.checks.${check}.title`)}
            </dt>
            <dd class="text-body-sm text-pretty text-ink-soft">{translate(`transparency.checks.${check}`)}</dd>
          </div>
        {/each}
      </dl>
    </section>

    <section aria-labelledby="transparency-others" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-others', 'eye', translate('transparency.others.title'))}
      {@render entries([
        {
          title: translate('transparency.others.hosting.title'),
          text: translate('transparency.others.hosting'),
        },
        {
          title: translate('transparency.others.client.title'),
          text: translate('transparency.others.client'),
        },
        ...(official
          ? [
              {
                title: translate('transparency.others.analytics.title'),
                text: translate('transparency.others.analytics'),
              },
            ]
          : []),
      ])}
    </section>

    <section aria-labelledby="transparency-personal" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-personal', 'person', translate('transparency.personal.title'))}
      {@render entries(
        PERSONAL.map((name) => ({
          title: translate(`transparency.personal.${name}.title`),
          text: translate(`transparency.personal.${name}`),
        })),
      )}
    </section>

    <section aria-labelledby="transparency-limits" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-limits', 'gauge', translate('transparency.limits.title'))}
      <dl class="mt-6 grid grid-cols-3 gap-2 sm:gap-3">
        {#each stats(data) as stat (stat.key)}
          <div class={[CARD, 'px-3 py-3 sm:px-4 sm:py-3.5']}>
            <dt class="text-micro text-ink-mute">{translate(stat.key)}</dt>
            <dd class="mt-1 text-stat font-light whitespace-nowrap text-ink tabular-nums">{stat.value}</dd>
          </div>
        {/each}
      </dl>
      <dl class={[CARD, 'mt-3 px-4']}>
        {#each rates(data) as rate (rate.key)}
          <div
            class="flex flex-col gap-0.5 border-t border-white/8 py-3 first:border-t-0 sm:flex-row sm:items-baseline sm:justify-between sm:gap-6"
          >
            <dt class="text-ui text-ink-mute">{translate(rate.key)}</dt>
            <dd class="text-ui text-ink sm:text-right">{rate.value}</dd>
          </div>
        {/each}
      </dl>
    </section>

    <section aria-labelledby="transparency-verify" class="animate-appear border-t border-white/8 pt-8">
      {@render heading('transparency-verify', 'shieldCheck', translate('transparency.verify.title'))}
      <dl class="mt-6 grid gap-3 sm:grid-cols-2">
        {@render fact(translate('transparency.client.version'), BUILD_INFO.version, 'client-version')}
        {@render fact(translate('transparency.server.version'), data.server, 'server-version')}
        {@render fact(translate('transparency.client.commit'), BUILD_INFO.commit, 'client-commit', true)}
      </dl>
      <div class="mt-6">
        <div class="flex items-baseline justify-between gap-4">
          <h3 id="transparency-files" class="text-title-sm text-ink">
            {translate('transparency.files.title')}
          </h3>
          <p class="text-micro text-ink-mute tabular-nums">
            {translator.plural('count.file', data.files.length)} · SHA-256
          </p>
        </div>
        <p class="mt-1 text-body-sm text-pretty text-ink-soft">{translate('transparency.files')}</p>
        <div class="relative mt-3 rounded-pane well">
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <div
            class="max-h-72 overflow-auto px-4 py-1.5"
            role="region"
            aria-labelledby="transparency-files"
            tabindex="0"
          >
            <ul role="list">
              {#each data.files as file (file.path)}
                <li class="border-t border-white/8 py-2.5 first:border-t-0">
                  <p class="text-caption wrap-anywhere text-ink-soft">{file.path}</p>
                  <p class="mt-0.5 text-caption wrap-anywhere text-ink-mute tabular-nums">{file.sha256}</p>
                </li>
              {/each}
            </ul>
          </div>
          <div
            class="pointer-events-none absolute inset-x-0 bottom-0 h-10 rounded-b-pane bg-linear-to-t from-void-950/90 to-transparent"
            aria-hidden="true"
          ></div>
        </div>
      </div>
      <div class="mt-6">
        <p class="text-body-sm text-pretty text-ink-soft">{translate('transparency.verify.command')}</p>
        <div class="mt-3 flex items-center gap-3 rounded-pane py-2 pr-2 pl-4 well">
          <Icon name="terminal" class="size-[18px] shrink-0 text-aegis-300" />
          <p class="min-w-0 flex-1 text-body-sm wrap-anywhere text-aegis-200 tabular-nums" translate="no">{command}</p>
          <CopyButton text={command} class="max-sm:w-11 max-sm:px-0" labelClass="max-sm:sr-only" />
        </div>
      </div>
      {@render caution(translate('transparency.verify.limit.title'), translate('transparency.verify.limit'))}
      <div class="mt-6">
        <h3 class="text-title-sm text-ink">{translate('transparency.verify.apps.title')}</h3>
        <p class="mt-1 text-body-sm text-pretty text-ink-soft">{translate('transparency.verify.apps')}</p>
        <Button variant="tint" size="compact" href={VERIFIED_CLIENTS_URL} rel="noreferrer" class="mt-3">
          {translate('operations.apps.link')}
          <Icon name="external" class="size-4 shrink-0 text-aegis-300" animated />
        </Button>
      </div>
    </section>

    <div class="border-t border-white/8 pt-8">
      <Button
        variant="ghost"
        href={REPOSITORY_URL}
        rel="noreferrer"
        class="max-sm:h-auto max-sm:min-h-11 max-sm:py-2.5 max-sm:text-left max-sm:whitespace-normal"
      >
        <GithubMark class="size-4 shrink-0" />
        {translate('transparency.source')}
      </Button>
    </div>
  {/if}
</section>
