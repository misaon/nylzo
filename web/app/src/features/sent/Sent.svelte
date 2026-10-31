<script lang="ts">
  import { tick } from 'svelte';

  import type { Status } from '../../shared/api/api.ts';
  import type { MessageKey } from '../../shared/language/czech.ts';

  import { createApi } from '../../shared/api/api.ts';
  import { formatDays, formatTime, formatWait } from '../../shared/language/format.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { RETENTION_DAYS } from '../../shared/sent-records/sent-record.ts';
  import { browserSentStore } from '../../shared/sent-records/sent-store.ts';
  import ConfirmDialog from '../../shared/ui/ConfirmDialog.svelte';
  import { FocusTargets, keepFocus } from '../../shared/ui/focus.ts';
  import {
    PAGE_ALERT,
    PAGE_BUTTON,
    PAGE_FACT_DETAIL,
    PAGE_FACT_TERM,
    PAGE_FACTS,
    PAGE_HEADING,
    PAGE_LIST,
    PAGE_LIST_ITEM,
    PAGE_STATUS,
    PAGE_TEXT,
    PAGE_TITLE,
  } from '../../shared/ui/page-classes.ts';
  import { type CancelOutcome, createSentPage, type ItemState, type SentItem, type SentView } from './sent-page.ts';

  type Confirming = { readonly kind: 'cancel' | 'forget'; readonly id: string } | { readonly kind: 'forget_all' };

  const DIALOGS: Record<
    Confirming['kind'],
    { readonly message: MessageKey; readonly confirm: MessageKey; readonly dismiss: MessageKey }
  > = {
    cancel: { message: 'sent.cancel.confirm', confirm: 'sent.cancel.yes', dismiss: 'sent.cancel.no' },
    forget: { message: 'sent.forget.confirm', confirm: 'sent.forget.yes', dismiss: 'sent.keep' },
    forget_all: { message: 'sent.forget_all.confirm', confirm: 'sent.forget_all.yes', dismiss: 'sent.keep' },
  };

  const translator = getTranslator();
  const { translate } = translator;

  let view = $state.raw<SentView>({ view: 'loading' });
  let confirming = $state.raw<Confirming>();
  let cancelled = $state.raw<CancelOutcome>();
  let storageFailed = $state(false);
  const focus = new FocusTargets<string>();
  const dialog = $derived(confirming ? DIALOGS[confirming.kind] : undefined);

  const sent = createSentPage({ api: createApi(), store: browserSentStore(), now: () => Date.now() }, (next) => {
    view = next;
  });

  const status = $derived.by(() => {
    if (view.view === 'loading') {
      return translate('sent.loading');
    }
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

  void sent.load();

  const time = (value: string | number) =>
    formatTime(typeof value === 'number' ? new Date(value).toISOString() : value, translator.language);
  const headingId = (id: string) => `sent-item-${id}`;

  function stateText(state: ItemState): string {
    switch (state.kind) {
      case 'loading': {
        return translate('sent.loading_item');
      }
      case 'loaded': {
        return translate(`sent.state.${state.status.state}`);
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
    }
  }

  function timeText(item: SentItem): string {
    const status = item.state.kind === 'loaded' ? item.state.status : undefined;
    if (status?.endedAt) {
      return translate('sent.ended_at', { time: time(status.endedAt) });
    }
    return item.ended ? translate('sent.ending') : translate('sent.until', { time: time(item.expiresAt) });
  }

  function firstOpenedText(state: ItemState): string {
    if (state.kind !== 'loaded') {
      return '';
    }
    return state.status.firstOpenedAt ? time(state.status.firstOpenedAt) : translate('sent.not_opened');
  }

  function counterText(state: ItemState, value: (status: Status) => string): string {
    if (state.kind !== 'loaded') {
      return '';
    }
    return state.status.endedAt ? translate('sent.not_kept') : value(state.status);
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
    void afterAction(current && current.kind !== 'forget_all' ? current.id : undefined);
  }

  function askForget(item: SentItem) {
    if (item.cancellable) {
      confirming = { kind: 'forget', id: item.id };
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
        cancelled = await sent.cancel(current.id);
      }, current.id);
    } else if (current.kind === 'forget') {
      await run(() => sent.forget(current.id));
    } else {
      await run(() => sent.forgetAll());
    }
  }
</script>

<p role="status" aria-live="polite" class={PAGE_STATUS}>{status}</p>

<section aria-labelledby="sent-title">
  <h2 id="sent-title" tabindex="-1" class={PAGE_TITLE} {@attach focus.attach('title')}>{translate('sent.title')}</h2>
  <p class={PAGE_TEXT}>{translate('sent.note', { days: formatDays(RETENTION_DAYS, translator.language) })}</p>

  {#if view.view === 'unavailable'}
    <p role="alert" class={PAGE_ALERT}>{translate('sent.unavailable')}</p>
  {:else if view.view === 'list' && view.items.length === 0}
    <p class={PAGE_TEXT}>{translate('sent.empty')}</p>
  {:else if view.view === 'list'}
    <ul class={PAGE_LIST}>
      {#each view.items as item (item.id)}
        <li class={[PAGE_LIST_ITEM, 'mt-4 rounded-pane bg-white/3 p-5 inset-ring inset-ring-white/8']}>
          <h3 id={headingId(item.id)} tabindex="-1" class={PAGE_HEADING} {@attach focus.attach(headingId(item.id))}>
            {translate('sent.item', { time: time(item.createdAt) })}
          </h3>
          <dl class={PAGE_FACTS}>
            <dt class={PAGE_FACT_TERM}>{translate('sent.label.state')}</dt>
            <dd class={PAGE_FACT_DETAIL}>{stateText(item.state)}</dd>
            <dt class={PAGE_FACT_TERM}>{translate('sent.label.time')}</dt>
            <dd class={PAGE_FACT_DETAIL}>{timeText(item)}</dd>
            <dt class={PAGE_FACT_TERM}>{translate('sent.label.first_opened')}</dt>
            <dd class={PAGE_FACT_DETAIL}>{firstOpenedText(item.state)}</dd>
            <dt class={PAGE_FACT_TERM}>{translate('sent.label.opens')}</dt>
            <dd class={PAGE_FACT_DETAIL}>
              {counterText(item.state, (status) =>
                translate('sent.opens', { used: status.opensUsed, max: status.opensMax }),
              )}
            </dd>
            <dt class={PAGE_FACT_TERM}>{translate('sent.label.attempts')}</dt>
            <dd class={PAGE_FACT_DETAIL}>
              {counterText(item.state, (status) => String(status.failedPassphraseAttempts))}
            </dd>
          </dl>
          <button
            type="button"
            class={PAGE_BUTTON}
            aria-describedby={headingId(item.id)}
            disabled={!item.cancellable}
            onclick={() => (confirming = { kind: 'cancel', id: item.id })}>{translate('sent.cancel')}</button
          >
          <button
            type="button"
            class={PAGE_BUTTON}
            aria-describedby={headingId(item.id)}
            onclick={() => askForget(item)}>{translate('sent.forget')}</button
          >
          {#if item.state.kind === 'retry_later' || item.state.kind === 'failed'}
            <button
              type="button"
              class={PAGE_BUTTON}
              aria-describedby={headingId(item.id)}
              onclick={() => run(() => sent.refresh(item.id), item.id)}>{translate('action.retry')}</button
            >
          {/if}
        </li>
      {/each}
    </ul>
    <button type="button" class={PAGE_BUTTON} onclick={() => (confirming = { kind: 'forget_all' })}
      >{translate('sent.forget_all')}</button
    >
  {/if}
</section>

{#if dialog}
  <ConfirmDialog
    id="sent-confirm"
    message={translate(dialog.message)}
    confirm={translate(dialog.confirm)}
    dismiss={translate(dialog.dismiss)}
    onconfirm={confirmed}
    onclose={dismissed}
  />
{/if}
