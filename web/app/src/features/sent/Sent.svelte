<script lang="ts">
  import { tick } from 'svelte';

  import type { MessageState } from '../../shared/api/api-errors.ts';
  import { createApi } from '../../shared/api/api.ts';
  import type { MessageKey } from '../../shared/language/czech.ts';
  import { formatDays, formatMoment, formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { RETENTION_DAYS } from '../../shared/sent-records/sent-record.ts';
  import { browserSentStore } from '../../shared/sent-records/sent-store.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import ConfirmDialog from '../../shared/ui/ConfirmDialog.svelte';
  import { FocusTargets, keepFocus } from '../../shared/ui/focus.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import { createSentPage } from './sent-page.ts';
  import type { CancelOutcome, ItemState, SentItem, SentView } from './sent-page.ts';

  type Confirming = { readonly kind: 'cancel' | 'forget'; readonly item: SentItem } | { readonly kind: 'forget_all' };

  const translator = getTranslator();
  const { translate } = translator;

  let view = $state.raw<SentView>({ view: 'loading' });
  let confirming = $state.raw<Confirming>();
  let cancelled = $state.raw<CancelOutcome>();
  let storageFailed = $state(false);
  const focus = new FocusTargets<string>();
  const retention = $derived(formatDays(RETENTION_DAYS, translator.language));
  const listed = $derived(view.view === 'list' && view.items.length > 0);

  const sent = createSentPage({ api: createApi(), store: browserSentStore(), now: () => Date.now() }, (next) => {
    view = next;
  });

  const status = $derived.by(() => {
    if (storageFailed) {
      return translate('sent.unavailable');
    }
    if (cancelled?.outcome === 'cancelled') {
      return translate('sent.cancelled');
    }
    if (cancelled?.outcome === 'retry_later') {
      return translate('read.retry_later', { time: formatWait(cancelled.retryAfter, translator.language) });
    }
    return cancelled?.outcome === 'failed' ? translate('sent.failed') : '';
  });

  const dialog = $derived.by(() => {
    if (confirming?.kind === 'cancel') {
      const { label } = confirming.item;
      return {
        title: label ? translate('sent.cancel.title', { label }) : translate('sent.cancel.title.untitled'),
        message: translate('sent.cancel.confirm'),
        confirm: translate('sent.cancel.yes'),
        dismiss: translate('sent.cancel.no'),
        icon: 'trash',
      } as const;
    }
    if (!confirming) {
      return;
    }
    return {
      title: translate(confirming.kind === 'forget' ? 'sent.forget.title' : 'sent.forget_all.title'),
      message: translate(confirming.kind === 'forget' ? 'sent.forget.confirm' : 'sent.forget_all.confirm'),
      confirm: translate(confirming.kind === 'forget' ? 'sent.forget.yes' : 'sent.forget_all.yes'),
      dismiss: translate('sent.keep'),
      icon: 'close',
    } as const;
  });

  void sent.load();

  const time = (value: string | number) =>
    formatMoment(typeof value === 'number' ? new Date(value).toISOString() : value, translator.language);
  const headingId = (id: string) => `sent-item-${id}`;

  function endedState(state: MessageState): Exclude<MessageState, 'available'> {
    return state === 'available' ? 'expired' : state;
  }

  function badgeOf(item: SentItem): { readonly key: MessageKey; readonly tone: 'live' | 'quiet' | 'problem' } {
    const { state } = item;
    if (state.kind === 'loaded' && !item.ended) {
      return { key: 'sent.state.available', tone: 'live' };
    }
    if (state.kind === 'loaded') {
      return { key: `sent.state.${endedState(state.status.state)}`, tone: 'quiet' };
    }
    if (state.kind === 'unknown') {
      return { key: 'sent.state.unknown', tone: 'quiet' };
    }
    return state.kind === 'loading'
      ? { key: 'sent.state.loading', tone: 'quiet' }
      : { key: 'sent.state.failed', tone: 'problem' };
  }

  function summaryOf(item: SentItem): string {
    const { state } = item;
    switch (state.kind) {
      case 'loading': {
        return translate('sent.state.loading');
      }
      case 'unknown': {
        return translate('sent.unknown');
      }
      case 'retry_later': {
        return translate('read.retry_later', { time: formatWait(state.retryAfter, translator.language) });
      }
      case 'failed': {
        return translate('sent.failed');
      }
      case 'loaded': {
        const ended = state.status.endedAt ?? new Date(item.expiresAt).toISOString();
        return translate(`sent.ended.${endedState(state.status.state)}`, { time: time(ended), days: retention });
      }
    }
  }

  async function afterAction(id?: string) {
    await tick();
    keepFocus((id === undefined ? undefined : focus.get(headingId(id))) ?? focus.get('title'));
  }

  async function run(action: () => Promise<unknown>, id?: string) {
    storageFailed = false;
    try {
      await action();
    } catch {
      storageFailed = true;
    }
    await afterAction(id);
  }

  function dismissed() {
    const current = confirming;
    confirming = undefined;
    void afterAction(current && current.kind !== 'forget_all' ? current.item.id : undefined);
  }

  function askForget(item: SentItem) {
    if (item.cancellable) {
      confirming = { kind: 'forget', item };
    } else {
      void run(() => sent.forget(item.id));
    }
  }

  async function confirmed() {
    const current = confirming;
    confirming = undefined;
    cancelled = undefined;
    if (!current) {
      return;
    }
    if (current.kind === 'cancel') {
      await run(async () => {
        cancelled = await sent.cancel(current.item.id);
      }, current.item.id);
    } else if (current.kind === 'forget') {
      await run(() => sent.forget(current.item.id));
    } else {
      await run(() => sent.forgetAll());
    }
  }
</script>

{#snippet emptyCard(icon: 'inbox' | 'alert', title: string, text: string, tone: string)}
  <span class={['grid size-14 place-items-center rounded-full', tone]} aria-hidden="true">
    <Icon name={icon} class="size-6" stroke={1.6} />
  </span>
  <p class="mt-5 text-title-sm font-medium text-ink">{title}</p>
  <p role={icon === 'alert' ? 'alert' : undefined} class="mt-1.5 max-w-[380px] text-body-sm text-pretty text-ink-mute">
    {text}
  </p>
  <Button variant="primary" href="/" class="mt-6">
    <Icon name="pen" class="size-4 shrink-0" animated />
    <span class="relative">{translate('nav.write')}</span>
  </Button>
{/snippet}

<p
  role="status"
  aria-live="polite"
  class={cancelled?.outcome === 'cancelled' ? 'sr-only' : 'mb-4 text-body text-aegis-200 empty:hidden'}
>
  {status}
</p>

<section aria-labelledby="sent-title">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
    <div class="min-w-0">
      <h2
        id="sent-title"
        tabindex="-1"
        class="font-display text-page font-light text-ink outline-none md:text-page-lg"
        {@attach focus.attach('title')}
      >
        {translate('sent.title')}
      </h2>
      <p class="mt-2 max-w-[540px] text-body text-pretty text-ink-soft">
        {translate('sent.note', { days: retention })}
      </p>
    </div>
    <Button
      variant="ghost"
      size="compact"
      class={['self-start sm:self-auto', !listed && 'invisible max-sm:hidden']}
      onclick={() => (confirming = { kind: 'forget_all' })}
    >
      <Icon name="close" class="size-4 shrink-0" animated />
      {translate('sent.forget_all')}
    </Button>
  </div>

  {#if listed && view.view === 'list'}
    <ul class="mt-8 grid gap-3">
      {#each view.items as item (item.id)}
        {const badge = $derived(badgeOf(item))}
        {const pending = $derived(item.state.kind === 'loading')}
        {const live = $derived(pending || (item.state.kind === 'loaded' && !item.ended))}
        <li class="rounded-pane bg-white/4 p-4 shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] ring-1 ring-white/8 sm:p-6">
          <div
            class="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3.5 sm:grid-cols-[auto_minmax(0,1fr)_auto]"
          >
            <span
              class={[
                'grid size-10 shrink-0 place-items-center rounded-full',
                item.ended ? 'text-ink-soft tile-neutral' : 'text-white step-current',
              ]}
              aria-hidden="true"
            >
              <Icon name={item.ended ? 'envelopeOpen' : 'envelope'} class="size-[18px]" />
            </span>
            <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 max-sm:gap-x-2.5">
              <h3
                id={headingId(item.id)}
                tabindex="-1"
                class="text-title-sm font-medium text-ink outline-none max-sm:basis-full"
                {@attach focus.attach(headingId(item.id))}
              >
                {item.label ?? translate('sent.item')}
              </h3>
              <span
                class={[
                  'items-center gap-1.5 rounded-full px-2.5 py-1 text-micro font-medium ring-1',
                  pending ? 'inline-grid' : 'inline-flex',
                  badge.tone === 'live' && 'bg-aegis-500/12 text-aegis-200 ring-aegis-400/35',
                  badge.tone === 'quiet' && 'bg-white/6 text-ink-soft ring-white/12',
                  badge.tone === 'problem' && 'bg-rose-500/12 text-rose-200 ring-rose-300/35',
                ]}
              >
                {#if pending}
                  <span class="invisible inline-flex items-center gap-1.5 [grid-area:1/1]">
                    <span class="size-1.5"></span>{translate('sent.state.available')}
                  </span>
                  <span class="[grid-area:1/1]">{translate(badge.key)}</span>
                {:else if badge.tone === 'live'}
                  <span
                    class="size-1.5 animate-pulse-core rounded-full bg-aegis-300 shadow-[0_0_8px_oklch(0.86_0.06_246/0.9)]"
                  ></span>
                {:else if item.state.kind === 'loaded' && item.state.status.state === 'opened'}
                  <Icon name="check" class="size-3.5 shrink-0" />
                {/if}
                {#if !pending}{translate(badge.key)}{/if}
              </span>
              <span class="text-caption text-ink-mute sm:hidden">
                {translate('sent.created', { time: time(item.createdAt) })}
              </span>
            </div>
            <p class="col-start-2 mt-0.5 text-caption text-ink-mute max-sm:hidden">
              {translate('sent.created', { time: time(item.createdAt) })}
            </p>
            {#if live}
              {const known = $derived(item.state.kind === 'loaded' ? item.state.status : undefined)}
              {const facts = $derived([
                { key: 'sent.label.time', value: translate('sent.until', { time: time(item.expiresAt) }) },
                {
                  key: 'sent.label.opens',
                  value: known && translate('sent.opens', { used: known.opensUsed, max: known.opensMax }),
                },
                {
                  key: 'sent.label.first_opened',
                  value: known && (known.firstOpenedAt ? time(known.firstOpenedAt) : translate('sent.not_opened')),
                },
                { key: 'sent.label.attempts', value: known && String(known.failedPassphraseAttempts) },
              ] as const)}
              <dl
                class="col-span-2 mt-4 grid grid-cols-2 gap-x-4 gap-y-3 rounded-pane bg-white/3 p-3.5 ring-1 ring-white/6 sm:col-span-2 sm:col-start-2 sm:mt-5 sm:gap-x-6 sm:gap-y-3.5 sm:p-4 md:grid-cols-[minmax(0,1.35fr)_repeat(3,minmax(0,1fr))]"
              >
                {#each facts as fact (fact.key)}
                  <div class="min-w-0">
                    <dt class="text-micro text-ink-mute">{translate(fact.key)}</dt>
                    <dd class="mt-0.5 min-h-5 text-ui text-ink sm:mt-1">{fact.value}</dd>
                  </div>
                {/each}
              </dl>
            {:else}
              <p
                class={[
                  'col-span-2 mt-3 text-ui text-pretty max-sm:text-body-sm sm:col-span-2 sm:col-start-2',
                  badge.tone === 'problem' ? 'text-rose-200' : 'text-ink-soft',
                ]}
              >
                {summaryOf(item)}
              </p>
            {/if}
            <div
              class="col-span-2 mt-4 flex flex-wrap items-center gap-2 border-t border-white/8 pt-3 sm:col-span-1 sm:col-start-3 sm:row-span-2 sm:row-start-1 sm:mt-0 sm:justify-end sm:self-center sm:border-0 sm:pt-0"
            >
              {#if item.state.kind === 'retry_later' || item.state.kind === 'failed'}
                <Button
                  variant="tint"
                  size="compact"
                  class="max-sm:flex-auto"
                  onclick={() => run(() => sent.refresh(item.id), item.id)}
                >
                  <Icon name="refresh" class="size-4 shrink-0" animated />
                  {translate('action.retry')}
                </Button>
              {/if}
              {#if item.cancellable || pending}
                <Button
                  variant="danger"
                  size="compact"
                  aria-describedby={headingId(item.id)}
                  class={['max-sm:flex-auto', pending && 'invisible']}
                  onclick={() => (confirming = { kind: 'cancel', item })}
                >
                  <Icon name="trash" class="size-4 shrink-0" animated />
                  {translate('sent.cancel')}
                </Button>
              {/if}
              <Button
                variant="ghost"
                size="compact"
                aria-describedby={headingId(item.id)}
                class="transition duration-300 ease-silk max-sm:flex-auto"
                onclick={() => askForget(item)}
              >
                <Icon name="close" class="size-4 shrink-0" animated />
                <span
                  >{translate('sent.forget')}<span class="max-sm:sr-only">{translate('sent.forget.suffix')}</span></span
                >
              </Button>
            </div>
          </div>
        </li>
      {/each}
    </ul>
  {:else}
    <div
      class="mt-8 flex flex-col items-center rounded-pane bg-white/4 px-6 py-12 text-center shadow-[inset_0_1px_0_oklch(1_0_0/0.06)] ring-1 ring-white/8 md:py-14"
    >
      {#if view.view === 'loading'}
        <span class="grid size-14 place-items-center text-aegis-300" aria-hidden="true">
          <svg
            class="size-8 motion-safe:animate-spin"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
          >
            <circle cx="12" cy="12" r="9" stroke-opacity="0.2"></circle>
            <path d="M21 12a9 9 0 0 0-9-9"></path>
          </svg>
        </span>
        <p role="status" class="mt-5 text-body text-ink-soft">{translate('sent.loading')}</p>
      {:else if view.view === 'unavailable'}
        {@render emptyCard(
          'alert',
          translate('sent.unavailable.title'),
          translate('sent.unavailable'),
          'tile-warning text-white',
        )}
      {:else}
        {@render emptyCard(
          'inbox',
          translate('sent.empty.title'),
          translate('sent.empty'),
          'tile-neutral text-ink-soft',
        )}
      {/if}
    </div>
  {/if}
</section>

{#if dialog}
  <ConfirmDialog
    id="sent-confirm"
    title={dialog.title}
    message={dialog.message}
    confirm={dialog.confirm}
    dismiss={dialog.dismiss}
    icon={dialog.icon}
    onconfirm={confirmed}
    onclose={dismissed}
  />
{/if}
