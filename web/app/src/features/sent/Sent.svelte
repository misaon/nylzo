<script lang="ts">
  import { flushSync, onDestroy, tick } from 'svelte';
  import { flip } from 'svelte/animate';
  import { cubicOut } from 'svelte/easing';
  import { fade } from 'svelte/transition';

  import type { MessageState } from '../../shared/api/api-errors.ts';
  import { createApi } from '../../shared/api/api.ts';
  import type { MessageKey } from '../../shared/language/catalog.ts';
  import { formatMoment, formatWait } from '../../shared/language/format.ts';
  import { homePath } from '../../shared/language/language.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { servedByLocalClient } from '../../shared/local-client/instance-origin.ts';
  import { RETENTION_DAYS } from '../../shared/sent-records/sent-record.ts';
  import { browserSentStore } from '../../shared/sent-records/sent-store.ts';
  import { getSessionSentStore } from '../../shared/sent-records/session-sent-store.ts';
  import Button from '../../shared/ui/button/Button.svelte';
  import ConfirmDialog from '../../shared/ui/dialog/ConfirmDialog.svelte';
  import { FocusTargets, keepFocus } from '../../shared/ui/focus/focus.ts';
  import Icon from '../../shared/ui/icon/Icon.svelte';
  import StateCard from '../../shared/ui/notice/StateCard.svelte';
  import { motionMs } from '../../shared/ui/transition/motion.ts';
  import { createSentPage } from './sent-page.ts';
  import type { CancelOutcome, ItemState, SentItem, SentView } from './sent-page.ts';

  interface Fact {
    readonly icon: 'alert' | 'check' | 'clock' | 'eye';
    readonly text: string;
    readonly warning?: boolean;
  }

  type Confirming = { readonly kind: 'cancel'; readonly item: SentItem } | { readonly kind: 'forget_all' };

  const translator = getTranslator();
  const { translate } = translator;

  let view = $state.raw<SentView>({ view: 'loading' });
  let confirming = $state.raw<Confirming>();
  let cancelled = $state.raw<{ readonly id: string; readonly outcome: CancelOutcome }>();
  let storageFailed = $state(false);
  const focus = new FocusTargets<string>();
  const retention = $derived(translator.plural('count.day', RETENTION_DAYS));
  const items = $derived(view.view === 'list' ? view.items : []);
  let listedIds = '';
  // Items glide only when the list itself changes; a status change resizes its own item with a slide.
  const listChanged = $derived.by(() => {
    const ids = items.map((item) => item.id).join(' ');
    const changed = listedIds !== '' && ids !== listedIds;
    listedIds = ids;
    return changed;
  });

  // A session of the local client has an origin nobody purges later, so it opens no storage (Protocol §14.1).
  const browser = servedByLocalClient() ? undefined : browserSentStore();
  const sent = createSentPage(
    { api: createApi(), session: getSessionSentStore(), browser, now: () => Date.now() },
    (next) => {
      view = next;
    },
  );

  function cancelText(outcome: CancelOutcome): string {
    switch (outcome.outcome) {
      case 'cancelled': {
        return translate('sent.cancelled');
      }
      case 'retry_later': {
        return translate('sent.cancel.retry_later', { time: formatWait(outcome.retryAfter, translator.plural) });
      }
      case 'failed': {
        return translate('sent.cancel.failed');
      }
      case 'ended': {
        return translate('sent.cancel.ended');
      }
      case 'unknown':
      case 'ignored': {
        return '';
      }
    }
  }

  const storageProblem = $derived(storageFailed || (view.view === 'list' && view.storageFailed));
  const status = $derived.by(() => {
    if (storageProblem) {
      return translate('sent.unavailable');
    }
    return cancelled ? cancelText(cancelled.outcome) : '';
  });

  function cancelProblem(item: SentItem): string {
    if (cancelled?.id !== item.id || cancelled.outcome.outcome === 'cancelled') {
      return '';
    }
    return cancelText(cancelled.outcome);
  }

  function dialogOf(asked: Confirming) {
    switch (asked.kind) {
      case 'cancel': {
        const { label } = asked.item;
        return {
          title: label ? translate('sent.cancel.title', { label }) : translate('sent.cancel.title.untitled'),
          message: translate('sent.cancel.confirm'),
          confirmLabel: translate('sent.cancel'),
          dismissLabel: translate('sent.cancel.no'),
          icon: 'trash',
        } as const;
      }
      case 'forget_all': {
        return {
          title: translate('sent.forget_all.title'),
          message: translate('sent.forget_all.confirm'),
          confirmLabel: translate('sent.forget_all'),
          dismissLabel: translate('sent.keep'),
          icon: 'close',
        } as const;
      }
    }
  }

  const dialog = $derived(confirming && dialogOf(confirming));

  void sent.load();
  onDestroy(() => {
    sent.stop();
    browser?.close();
  });

  // A page kept for the back button must not hold the keys or labels it showed.
  function leave() {
    sent.stop();
    browser?.close();
    confirming = undefined;
    cancelled = undefined;
    flushSync();
  }

  const time = (moment: number | string) => formatMoment(moment, translator.language);
  const headingId = (id: string) => `sent-item-${id}`;

  function createdText(item: SentItem): string {
    return translate(item.session ? 'sent.created.session' : 'sent.created', { time: time(item.createdAt) });
  }

  function factsOf(item: SentItem): readonly Fact[] {
    const facts: Fact[] = [{ icon: 'clock', text: translate('sent.until', { time: time(item.expiresAt) }) }];
    if (item.state.kind !== 'loaded') {
      return facts;
    }
    const { opensUsed, opensMax, firstOpenedAt, failedPassphraseAttempts } = item.state.status;
    facts.push({ icon: 'eye', text: translate('sent.opens', { used: opensUsed, max: opensMax }) });
    if (firstOpenedAt) {
      facts.push({ icon: 'check', text: translate('sent.first_opened', { time: time(firstOpenedAt) }) });
    }
    if (failedPassphraseAttempts > 0) {
      facts.push({
        icon: 'alert',
        text: translate('sent.attempts', { count: failedPassphraseAttempts }),
        warning: true,
      });
    }
    return facts;
  }

  function endedState(state: MessageState): Exclude<MessageState, 'available'> {
    return state === 'available' ? 'expired' : state;
  }

  function badgeOf(item: SentItem): { readonly key: MessageKey; readonly tone: 'live' | 'quiet' | 'problem' } {
    const { state } = item;
    switch (state.kind) {
      case 'loaded': {
        return item.ended
          ? { key: `sent.state.${endedState(state.status.state)}`, tone: 'quiet' }
          : { key: 'sent.state.available', tone: 'live' };
      }
      case 'unknown': {
        return { key: 'sent.state.unknown', tone: 'quiet' };
      }
      case 'loading': {
        return { key: 'sent.state.loading', tone: 'quiet' };
      }
      case 'retry_later':
      case 'failed': {
        return { key: 'sent.state.failed', tone: 'problem' };
      }
    }
  }

  function summaryOf({
    state,
    expiresAt,
  }: {
    readonly state: Exclude<ItemState, { readonly kind: 'loading' }>;
    readonly expiresAt: number;
  }): string {
    switch (state.kind) {
      case 'unknown': {
        return translate('sent.unknown');
      }
      case 'retry_later': {
        return translate('read.retry_later', { time: formatWait(state.retryAfter, translator.plural) });
      }
      case 'failed': {
        return translate('sent.failed');
      }
      case 'loaded': {
        return translate(`sent.ended.${endedState(state.status.state)}`, {
          time: time(state.status.endedAt ?? expiresAt),
        });
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
    void afterAction(current?.kind === 'cancel' ? current.item.id : undefined);
  }

  async function forget(id: string): Promise<void> {
    const index = items.findIndex((item) => item.id === id);
    const neighbour = items[index + 1] ?? items[index - 1];
    await run(() => sent.forget(id));
    if (!items.some((item) => item.id === id)) {
      focus.focus(neighbour ? headingId(neighbour.id) : 'title');
    }
  }

  async function confirmed(): Promise<void> {
    const current = confirming;
    confirming = undefined;
    cancelled = undefined;
    if (!current) {
      return;
    }
    switch (current.kind) {
      case 'cancel': {
        return run(async () => {
          cancelled = { id: current.item.id, outcome: await sent.cancel(current.item.id) };
        }, current.item.id);
      }
      case 'forget_all': {
        return run(() => sent.forgetAll());
      }
    }
  }
</script>

<svelte:window onpagehide={leave} onpageshow={(event) => event.persisted && void sent.load()} />

{#snippet emptyCard({
  icon,
  title,
  text,
  tone,
}: {
  readonly icon: 'alert' | 'inbox';
  readonly title: string;
  readonly text: string;
  readonly tone: string;
})}
  <span class={['grid size-14 place-items-center rounded-full', tone]} aria-hidden="true">
    <Icon name={icon} class="size-6" stroke={1.6} />
  </span>
  <h2 class="mt-5 text-title-sm text-ink">{title}</h2>
  <p role={icon === 'alert' ? 'alert' : undefined} class="mt-1.5 max-w-[380px] text-body-sm text-pretty text-ink-mute">
    {text}
  </p>
  <Button variant="primary" href={homePath(translator.language)} class="mt-6">
    <Icon name="pen" class="size-4 shrink-0" animated />
    <span class="relative">{translate('nav.write')}</span>
  </Button>
{/snippet}

<p role="status" aria-live="polite" class={storageProblem ? 'mb-4 text-body text-aegis-200' : 'sr-only'}>
  {status}
</p>

<section aria-labelledby="sent-title">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between sm:gap-6">
    <div class="min-w-0">
      <h1
        id="sent-title"
        tabindex="-1"
        class="font-display text-page font-light text-ink outline-none md:text-page-lg"
        {@attach focus.attach('title')}
      >
        {translate('sent.title')}
      </h1>
      <p class="mt-2 max-w-[540px] text-body text-pretty text-ink-soft">
        {translate('sent.note', { days: retention })}
      </p>
    </div>
    <Button
      variant="ghost"
      size="compact"
      class={['self-start sm:self-auto', items.length === 0 && 'invisible max-sm:hidden']}
      onclick={() => (confirming = { kind: 'forget_all' })}
    >
      <Icon name="close" class="size-4 shrink-0" animated />{translate('sent.forget_all')}
    </Button>
  </div>

  {#if items.length > 0}
    <ul role="list" class="mt-8 animate-appear overflow-hidden rounded-pane bg-white/4 shadow-rim ring-1 ring-white/8">
      {#each items as item (item.id)}
        {const badge = $derived(badgeOf(item))}
        {const pending = $derived(item.state.kind === 'loading')}
        {const retryable = $derived(
          item.state.kind === 'retry_later' || item.state.kind === 'failed' || item.state.kind === 'unknown',
        )}
        <li
          class="border-white/8 not-first:border-t"
          animate:flip={{ duration: listChanged ? motionMs(500) : 0, easing: cubicOut }}
          out:fade={{ duration: motionMs(300) }}
        >
          <div
            class="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3.5 p-4 sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center sm:px-5 sm:py-4.5"
          >
            <span
              class={[
                'grid size-10 shrink-0 place-items-center rounded-full tile-blend',
                item.ended ? 'text-ink-soft tile-neutral' : 'text-white tile-current',
              ]}
              aria-hidden="true"
            >
              <Icon name={item.ended ? 'envelopeOpen' : 'envelope'} class="size-[18px]" />
            </span>
            <div class="min-w-0">
              <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
                <h2
                  id={headingId(item.id)}
                  tabindex="-1"
                  class="text-title-sm text-ink outline-none"
                  {@attach focus.attach(headingId(item.id))}
                >
                  {item.label ?? translate('sent.item')}
                </h2>
                <span
                  class={[
                    'items-center gap-1.5 rounded-full px-2.5 py-1 text-micro font-medium ring-1 transition-[background-color,color,box-shadow]',
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
                    <span class="size-1.5 animate-pulse-core rounded-full bg-aegis-300 shadow-glow"></span>
                  {:else if item.state.kind === 'loaded' && item.state.status.state === 'opened'}
                    <Icon name="check" class="size-3.5 shrink-0" />
                  {/if}
                  {#if !pending}{translate(badge.key)}{/if}
                </span>
              </div>
              {#if item.state.kind === 'loading' || (item.state.kind === 'loaded' && !item.ended)}
                <p class="mt-2 flex flex-wrap items-center gap-x-5 gap-y-1.5 text-caption text-ink-soft tabular-nums">
                  {#each factsOf(item) as fact (fact.icon)}
                    <span class={['flex items-center gap-1.5 whitespace-nowrap', fact.warning && 'text-rose-200']}>
                      <Icon name={fact.icon} class={['size-4 shrink-0', !fact.warning && 'text-ink-mute']} />{fact.text}
                    </span>
                  {/each}
                  <span class="whitespace-nowrap text-ink-mute">{createdText(item)}</span>
                </p>
              {:else}
                <p
                  class={[
                    'mt-2 text-caption text-pretty tabular-nums',
                    badge.tone === 'problem' ? 'text-rose-200' : 'text-ink-mute',
                  ]}
                >
                  {summaryOf({ state: item.state, expiresAt: item.expiresAt })}
                </p>
              {/if}
              {#if cancelProblem(item)}
                <p class="mt-2 flex items-start gap-1.5 text-caption text-pretty text-rose-200">
                  <Icon name="alert" class="mt-0.5 size-4 shrink-0" />{cancelProblem(item)}
                </p>
              {/if}
            </div>
            {#if retryable || item.cancellable || pending}
              <div
                class="col-span-2 mt-3 flex flex-wrap items-center gap-2 sm:col-span-1 sm:col-start-3 sm:mt-0 sm:justify-end"
              >
                {#if retryable}
                  <Button
                    variant="tint"
                    size="compact"
                    aria-describedby={headingId(item.id)}
                    class="max-sm:flex-auto"
                    onclick={() => void run(() => sent.refresh(item.id), item.id)}
                  >
                    <Icon name="refresh" class="size-4 shrink-0" animated />{translate('action.retry')}
                  </Button>
                {/if}
                {#if item.state.kind === 'unknown'}
                  <Button
                    variant="ghost"
                    size="compact"
                    aria-describedby={headingId(item.id)}
                    class="max-sm:flex-auto"
                    onclick={() => void forget(item.id)}
                  >
                    <Icon name="close" class="size-4 shrink-0" animated />{translate('sent.forget')}
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
                    <Icon name="trash" class="size-4 shrink-0" animated />{translate('sent.cancel')}
                  </Button>
                {/if}
              </div>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {:else if view.view === 'loading'}
    <StateCard class="mt-8" loading={translate('sent.loading')} />
  {:else}
    <StateCard class="mt-8">
      {#if view.view === 'unavailable'}
        {@render emptyCard({
          icon: 'alert',
          title: translate('sent.unavailable.title'),
          text: translate('sent.unavailable'),
          tone: 'tile-warning text-white',
        })}
      {:else}
        {@render emptyCard({
          icon: 'inbox',
          title: translate('sent.empty.title'),
          text: translate('sent.empty'),
          tone: 'tile-neutral text-ink-soft',
        })}
      {/if}
    </StateCard>
  {/if}
</section>

{#if dialog}
  <ConfirmDialog
    id="sent-confirm"
    title={dialog.title}
    message={dialog.message}
    confirmLabel={dialog.confirmLabel}
    dismissLabel={dialog.dismissLabel}
    icon={dialog.icon}
    onconfirm={() => void confirmed()}
    onclose={dismissed}
  />
{/if}
