<script lang="ts">
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { createApi } from '../lib/api.ts';
  import { formatDuration, formatTime } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { createSentPage, indexedDbSentStore, isCancellable, type CancelOutcome, type ItemState, type SentItem, type SentView } from '../lib/sent.ts';

  type Confirming = { readonly kind: 'cancel' | 'forget'; readonly id: string } | { readonly kind: 'forget_all' };

  const i18n = getI18n();
  const { t } = i18n;

  let view = $state.raw<SentView>({ view: 'loading' });
  let confirming = $state.raw<Confirming>();
  let status = $state('');

  const sent = createSentPage({ api: createApi(), store: indexedDbSentStore(), now: () => Date.now() }, (next) => {
    view = next;
  });

  sent.load();

  const time = (value: string | number) => formatTime(typeof value === 'number' ? new Date(value).toISOString() : value, i18n.language);

  function stateText(state: ItemState): string {
    switch (state.kind) {
      case 'loading':
        return t('sent.loading_item');
      case 'loaded':
        return t(`sent.state.${state.status.state}`);
      case 'unknown':
        return t('sent.unknown');
      case 'retry_later':
        return t('read.retry_later', { time: formatDuration(state.retryAfter ?? 60, i18n.language) });
      case 'failed':
        return t('sent.failed');
    }
  }

  function timeText(item: SentItem): string {
    if (item.state.kind === 'loaded' && item.state.status.endedAt) {
      return t('sent.ended_at', { time: time(item.state.status.endedAt) });
    }
    return t('sent.until', { time: time(item.expiresAt) });
  }

  function cancelText(outcome: CancelOutcome): string {
    switch (outcome.outcome) {
      case 'cancelled':
        return t('sent.cancelled');
      case 'retry_later':
        return t('read.retry_later', { time: formatDuration(outcome.retryAfter ?? 60, i18n.language) });
      case 'failed':
        return t('sent.failed');
      default:
        return '';
    }
  }

  function askForget(item: SentItem) {
    if (isCancellable(item.state)) {
      confirming = { kind: 'forget', id: item.id };
    } else {
      sent.forget(item.id).catch(() => (status = t('sent.unavailable')));
    }
  }

  async function confirmed() {
    const current = confirming;
    confirming = undefined;
    if (!current) {
      return;
    }
    try {
      if (current.kind === 'cancel') {
        status = cancelText(await sent.cancel(current.id));
      } else if (current.kind === 'forget') {
        await sent.forget(current.id);
      } else {
        await sent.forgetAll();
      }
    } catch {
      status = t('sent.unavailable');
    }
  }
</script>

<p role="status" aria-live="polite">{view.view === 'loading' ? t('sent.loading') : status}</p>

<section aria-labelledby="sent-title">
  <h2 id="sent-title">{t('sent.title')}</h2>
  <p>{t('sent.note')}</p>

  {#if view.view === 'unavailable'}
    <p role="alert">{t('sent.unavailable')}</p>
  {:else if view.view === 'list' && view.items.length === 0}
    <p>{t('sent.empty')}</p>
  {:else if view.view === 'list'}
    <ul>
      {#each view.items as item (item.id)}
        <li>
          <h3>{t('sent.item', { time: time(item.createdAt) })}</h3>
          <dl>
            <dt>{t('sent.label.state')}</dt>
            <dd>{stateText(item.state)}</dd>
            <dt>{t('sent.label.time')}</dt>
            <dd>{timeText(item)}</dd>
            <dt>{t('sent.label.opens')}</dt>
            <dd>{item.state.kind === 'loaded' ? t('sent.opens', { used: item.state.status.opensUsed, max: item.state.status.opensMax }) : ''}</dd>
            <dt>{t('sent.label.attempts')}</dt>
            <dd>{item.state.kind === 'loaded' ? item.state.status.failedPassphraseAttempts : ''}</dd>
          </dl>
          <button type="button" disabled={!isCancellable(item.state)} onclick={() => (confirming = { kind: 'cancel', id: item.id })}>{t('sent.cancel')}</button>
          <button type="button" onclick={() => askForget(item)}>{t('sent.forget')}</button>
          {#if item.state.kind === 'retry_later' || item.state.kind === 'failed'}
            <button type="button" onclick={() => sent.refresh(item.id)}>{t('action.retry')}</button>
          {/if}
        </li>
      {/each}
    </ul>
    <button type="button" onclick={() => (confirming = { kind: 'forget_all' })}>{t('sent.forget_all')}</button>
  {/if}
</section>

{#if confirming?.kind === 'cancel'}
  <ConfirmDialog
    id="sent-confirm"
    message={t('sent.cancel.confirm')}
    confirm={t('sent.cancel.yes')}
    dismiss={t('sent.cancel.no')}
    onconfirm={confirmed}
    onclose={() => (confirming = undefined)}
  />
{:else if confirming?.kind === 'forget'}
  <ConfirmDialog
    id="sent-confirm"
    message={t('sent.forget.confirm')}
    confirm={t('sent.forget.yes')}
    dismiss={t('sent.keep')}
    onconfirm={confirmed}
    onclose={() => (confirming = undefined)}
  />
{:else if confirming?.kind === 'forget_all'}
  <ConfirmDialog
    id="sent-confirm"
    message={t('sent.forget.confirm')}
    confirm={t('sent.forget_all.yes')}
    dismiss={t('sent.keep')}
    onconfirm={confirmed}
    onclose={() => (confirming = undefined)}
  />
{/if}
