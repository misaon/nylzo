<script lang="ts">
  import { tick } from 'svelte';

  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { createApi } from '../lib/api.ts';
  import { keepFocus } from '../lib/focus.ts';
  import { formatDays, formatTime, formatWait } from '../lib/format.ts';
  import { getI18n, type MessageKey } from '../lib/i18n.svelte.ts';
  import {
    RETENTION_DAYS,
    createSentPage,
    indexedDbSentStore,
    type CancelOutcome,
    type ItemState,
    type SentItem,
    type SentView,
  } from '../lib/sent.ts';

  type Confirming = { readonly kind: 'cancel' | 'forget'; readonly id: string } | { readonly kind: 'forget_all' };

  const DIALOGS: Record<
    Confirming['kind'],
    { readonly message: MessageKey; readonly confirm: MessageKey; readonly dismiss: MessageKey }
  > = {
    cancel: { message: 'sent.cancel.confirm', confirm: 'sent.cancel.yes', dismiss: 'sent.cancel.no' },
    forget: { message: 'sent.forget.confirm', confirm: 'sent.forget.yes', dismiss: 'sent.keep' },
    forget_all: { message: 'sent.forget_all.confirm', confirm: 'sent.forget_all.yes', dismiss: 'sent.keep' },
  };

  const i18n = getI18n();
  const { t } = i18n;

  let view = $state.raw<SentView>({ view: 'loading' });
  let confirming = $state.raw<Confirming>();
  let cancelled = $state.raw<CancelOutcome>();
  let storageFailed = $state(false);

  const sent = createSentPage({ api: createApi(), store: indexedDbSentStore(), now: () => Date.now() }, (next) => {
    view = next;
  });

  const status = $derived.by(() => {
    if (view.view === 'loading') {
      return t('sent.loading');
    }
    if (storageFailed) {
      return t('sent.unavailable');
    }
    switch (cancelled?.outcome) {
      case 'cancelled':
        return t('sent.cancelled');
      case 'retry_later':
        return t('read.retry_later', { time: formatWait(cancelled.retryAfter, i18n.language) });
      case 'failed':
        return t('sent.failed');
      default:
        return '';
    }
  });

  sent.load();

  const time = (value: string | number) =>
    formatTime(typeof value === 'number' ? new Date(value).toISOString() : value, i18n.language);
  const headingId = (id: string) => `sent-item-${id}`;

  function stateText(state: ItemState): string {
    switch (state.kind) {
      case 'loading':
        return t('sent.loading_item');
      case 'loaded':
        return t(`sent.state.${state.status.state}`);
      case 'unknown':
        return t('sent.unknown');
      case 'retry_later':
        return t('read.retry_later', { time: formatWait(state.retryAfter, i18n.language) });
      case 'failed':
        return t('sent.failed');
    }
  }

  function timeText(item: SentItem): string {
    const status = item.state.kind === 'loaded' ? item.state.status : undefined;
    if (status?.endedAt) {
      return t('sent.ended_at', { time: time(status.endedAt) });
    }
    return item.ended ? t('sent.ending') : t('sent.until', { time: time(item.expiresAt) });
  }

  function firstOpenedText(state: ItemState): string {
    if (state.kind !== 'loaded') {
      return '';
    }
    return state.status.firstOpenedAt ? time(state.status.firstOpenedAt) : t('sent.not_opened');
  }

  function counterText(
    state: ItemState,
    value: (status: Extract<ItemState, { kind: 'loaded' }>['status']) => string,
  ): string {
    if (state.kind !== 'loaded') {
      return '';
    }
    return state.status.endedAt ? t('sent.not_kept') : value(state.status);
  }

  async function afterAction(id?: string) {
    await tick();
    keepFocus(document.getElementById(id && document.getElementById(headingId(id)) ? headingId(id) : 'sent-title'));
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
    afterAction(current && current.kind !== 'forget_all' ? current.id : undefined);
  }

  function askForget(item: SentItem) {
    if (item.cancellable) {
      confirming = { kind: 'forget', id: item.id };
    } else {
      run(() => sent.forget(item.id));
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

<p role="status" aria-live="polite">{status}</p>

<section aria-labelledby="sent-title">
  <h2 id="sent-title" tabindex="-1">{t('sent.title')}</h2>
  <p>{t('sent.note', { days: formatDays(RETENTION_DAYS, i18n.language) })}</p>

  {#if view.view === 'unavailable'}
    <p role="alert">{t('sent.unavailable')}</p>
  {:else if view.view === 'list' && view.items.length === 0}
    <p>{t('sent.empty')}</p>
  {:else if view.view === 'list'}
    <ul>
      {#each view.items as item (item.id)}
        <li>
          <h3 id={headingId(item.id)} tabindex="-1">{t('sent.item', { time: time(item.createdAt) })}</h3>
          <dl>
            <dt>{t('sent.label.state')}</dt>
            <dd>{stateText(item.state)}</dd>
            <dt>{t('sent.label.time')}</dt>
            <dd>{timeText(item)}</dd>
            <dt>{t('sent.label.first_opened')}</dt>
            <dd>{firstOpenedText(item.state)}</dd>
            <dt>{t('sent.label.opens')}</dt>
            <dd>
              {counterText(item.state, (status) => t('sent.opens', { used: status.opensUsed, max: status.opensMax }))}
            </dd>
            <dt>{t('sent.label.attempts')}</dt>
            <dd>{counterText(item.state, (status) => String(status.failedPassphraseAttempts))}</dd>
          </dl>
          <button
            type="button"
            aria-describedby={headingId(item.id)}
            disabled={!item.cancellable}
            onclick={() => (confirming = { kind: 'cancel', id: item.id })}>{t('sent.cancel')}</button
          >
          <button type="button" aria-describedby={headingId(item.id)} onclick={() => askForget(item)}
            >{t('sent.forget')}</button
          >
          {#if item.state.kind === 'retry_later' || item.state.kind === 'failed'}
            <button
              type="button"
              aria-describedby={headingId(item.id)}
              onclick={() => run(() => sent.refresh(item.id), item.id)}>{t('action.retry')}</button
            >
          {/if}
        </li>
      {/each}
    </ul>
    <button type="button" onclick={() => (confirming = { kind: 'forget_all' })}>{t('sent.forget_all')}</button>
  {/if}
</section>

{#if confirming}
  {@const dialog = DIALOGS[confirming.kind]}
  <ConfirmDialog
    id="sent-confirm"
    message={t(dialog.message)}
    confirm={t(dialog.confirm)}
    dismiss={t(dialog.dismiss)}
    onconfirm={confirmed}
    onclose={dismissed}
  />
{/if}
