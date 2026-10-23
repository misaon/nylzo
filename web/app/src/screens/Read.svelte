<script lang="ts">
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';

  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import ReportForm from '../components/ReportForm.svelte';
  import { createApi } from '../lib/api.ts';
  import { fileExtension, isKnownSafeFile, saveFile } from '../lib/download.ts';
  import { formatBytes, formatTime, formatWait } from '../lib/format.ts';
  import { keepFocus } from '../lib/focus.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { scryptInWorker } from '../lib/kdf.ts';
  import { createReader, type BurnOutcome, type ReadView, type RevealedFile } from '../lib/read.ts';

  const i18n = getI18n();
  const { t } = i18n;

  let view = $state.raw<ReadView>({ view: 'loading' });
  let passphrase = $state('');
  let confirming = $state(false);
  let askingDelete = $state(false);
  let reporting = $state(false);
  let restDeleted = $state(false);
  let deleting = $state(false);
  let burned = $state.raw<{ readonly outcome: BurnOutcome; readonly afterReveal: boolean }>();

  const status = $derived(
    view.view === 'loading' ? t('read.loading') : view.view === 'busy' ? t(`busy.${view.step}`) : burned ? burnText(burned.outcome, burned.afterReveal) : '',
  );

  const reader = createReader(
    {
      api: createApi(),
      location,
      session: {
        getItem: (key) => sessionStorage.getItem(key),
        setItem: (key, value) => sessionStorage.setItem(key, value),
        removeItem: (key) => sessionStorage.removeItem(key),
      },
      page: {
        visible: () => document.visibilityState === 'visible',
        prerendering: () => (document as Document & { prerendering?: boolean }).prerendering === true,
      },
      dropFragment: () => history.replaceState(null, '', location.pathname),
      scrypt: (pass, saltPw, onProgress) => scryptInWorker(pass, saltPw, { onProgress }),
      now: () => Date.now(),
    },
    (next) => {
      view = next;
    },
  );

  reader.load();

  const focus: Attachment<HTMLElement> = (element) => {
    element.focus();
  };

  function reveal(event: SubmitEvent) {
    event.preventDefault();
    const entered = passphrase;
    passphrase = '';
    reader.reveal(entered);
  }

  function burnText(outcome: BurnOutcome, afterReveal: boolean): string {
    switch (outcome.outcome) {
      case 'deleted':
        return afterReveal ? t('read.deleted_rest') : '';
      case 'ended':
        return afterReveal ? t(`read.ended.${outcome.state}`) : '';
      case 'retry_later':
        return t('read.retry_later', { time: formatWait(outcome.retryAfter, i18n.language) });
      case 'unknown':
        return afterReveal ? t('read.delete.failed') : '';
      case 'failed':
        return t('read.delete.failed');
      case 'ignored':
        return '';
    }
  }

  async function burn() {
    askingDelete = false;
    burned = undefined;
    deleting = true;
    const afterReveal = view.view === 'revealed';
    const outcome = await reader.burn();
    deleting = false;
    restDeleted ||= afterReveal && (outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    burned = { outcome, afterReveal };
    await tick();
    keepFocus(document.getElementById(afterReveal ? 'revealed-title' : 'read-title'));
  }

  async function dismissed() {
    askingDelete = false;
    await tick();
    keepFocus(document.getElementById(view.view === 'revealed' ? 'revealed-title' : 'read-title'));
  }

  function leave() {
    burned = undefined;
    confirming = false;
    askingDelete = false;
    reporting = false;
    reader.clear();
  }

  function returned(event: PageTransitionEvent) {
    if (event.persisted) {
      location.reload();
    }
  }

  function download(file: RevealedFile) {
    if (isKnownSafeFile(file.name) || confirming) {
      confirming = false;
      saveFile(file.bytes, file.name);
    } else {
      confirming = true;
    }
  }
</script>

<svelte:window onpagehide={leave} onpageshow={returned} />

<p role="status" aria-live="polite">{status}</p>

<section aria-labelledby="read-title">
  <h2 id="read-title" tabindex="-1">{t('read.title')}</h2>

  {#if view.view === 'invalid'}
    <p role="alert">{t('read.invalid')}</p>
  {:else if view.view === 'unknown'}
    <p role="alert">{t('read.unknown')}</p>
  {:else if view.view === 'tampered'}
    <p role="alert">{t('read.tampered')}</p>
  {:else if view.view === 'cleared'}
    <p role="alert">{t('read.cleared')}</p>
  {:else if view.view === 'failed'}
    <p role="alert">{t('read.failed')}</p>
    <button type="button" onclick={() => reader.load()}>{t('action.retry')}</button>
  {:else if view.view === 'retry_later'}
    <p role="alert">{t('read.retry_later', { time: formatWait(view.retryAfter, i18n.language) })}</p>
    <button type="button" onclick={() => reader.load()}>{t('action.retry')}</button>
  {:else if view.view === 'ended'}
    <p role="alert">{t(`read.ended.${view.state}`)}</p>
    {#if view.firstOpenedAt}
      <p>{t('read.opened_at', { time: formatTime(view.firstOpenedAt, i18n.language) })}</p>
    {/if}
  {:else if view.view === 'ready'}
    <ul>
      {#if view.metadata.expiresAt}
        <li>{t('read.expires', { time: formatTime(view.metadata.expiresAt, i18n.language) })}</li>
      {/if}
      <li>{t('read.opens', { count: view.metadata.opensRemaining })}</li>
      {#if view.metadata.passphraseRequired}
        <li>{t('read.passphrase_required')}</li>
      {/if}
    </ul>
    {#if view.deviceFailed}
      <p role="alert">{t('read.device')}</p>
    {/if}
    {#if view.resumable}
      <p>{t('read.resumable')}</p>
    {/if}
    {#if view.leaseEnded}
      <p role="alert">{t('read.lease_ended')}</p>
    {/if}
    {#if view.rejected?.reason === 'malformed'}
      <p role="alert">{t('read.rejected.malformed')}</p>
    {:else if view.rejected?.reason === 'wrong_or_changed'}
      <p role="alert">{t('read.rejected.wrong_or_changed')}</p>
    {:else if view.rejected?.locked}
      <p role="alert">{t('read.rejected.locked')}</p>
    {:else if view.rejected && view.rejected.retryAfter > 0}
      <p role="alert">{t('read.rejected.wait', { time: formatWait(view.rejected.retryAfter, i18n.language) })}</p>
    {:else if view.rejected}
      <p role="alert">{t('read.rejected.wrong', { count: view.rejected.attemptsLeft })}</p>
    {/if}
    <form onsubmit={reveal}>
      {#if view.metadata.passphraseRequired}
        <label for="read-passphrase">{t('read.passphrase')}</label>
        <input
          id="read-passphrase"
          type="password"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          data-1p-ignore=""
          data-lpignore="true"
          data-bwignore="true"
          data-form-type="other"
          required
          bind:value={passphrase}
          {@attach focus}
        />
      {/if}
      <p>{t('read.reveal.note')}</p>
      <button type="submit" disabled={deleting}>{t('read.reveal')}</button>
    </form>
    <button type="button" disabled={deleting} onclick={() => (askingDelete = true)}>{t('read.delete')}</button>
    {@render reportArea()}
  {:else if view.view === 'busy'}
    {#if view.fraction === undefined}
      <progress aria-label={t(`busy.${view.step}`)}></progress>
    {:else}
      <progress aria-label={t(`busy.${view.step}`)} max="1" value={view.fraction}></progress>
    {/if}
  {:else if view.view === 'revealed'}
    <h3 id="revealed-title" tabindex="-1" {@attach focus}>{t('revealed.title')}</h3>
    {#if view.text !== undefined}
      <pre class="whitespace-pre-wrap break-words">{view.text}</pre>
    {/if}
    {#if view.file}
      {@const file = view.file}
      {@const extension = fileExtension(file.name)}
      <p>{t('revealed.file')}: <bdi>{file.name}</bdi>, {formatBytes(file.bytes.length, i18n.language)}</p>
      {#if confirming}
        <p role="alert">{t('revealed.confirm.title')}</p>
        <p>{extension ? t('revealed.confirm.extension', { extension }) : t('revealed.confirm.none')}</p>
        <button type="button" onclick={() => download(file)}>{t('revealed.confirm.download')}</button>
        <button type="button" onclick={() => (confirming = false)}>{t('revealed.confirm.cancel')}</button>
      {:else}
        <button type="button" onclick={() => download(file)}>{t('revealed.download')}</button>
      {/if}
    {/if}
    <p>{t('revealed.note')}</p>
    {#if view.burnable || restDeleted}
      <button type="button" disabled={!view.burnable || deleting} onclick={() => (askingDelete = true)}>{t('read.delete.rest')}</button>
    {/if}
    {#if view.burnable}
      {@render reportArea()}
    {/if}
  {/if}
</section>

{#snippet reportArea()}
  <button type="button" aria-expanded={reporting} onclick={() => (reporting = !reporting)}>{t('report.open')}</button>
  {#if reporting}
    <p>{t('report.note')}</p>
    <p>{t('report.privacy')}</p>
    <ReportForm name="read-report" send={(_link, reason) => reader.report(reason)} />
  {/if}
{/snippet}

{#if askingDelete}
  <ConfirmDialog
    id="read-confirm"
    message={t('read.delete.confirm')}
    confirm={t('read.delete.yes')}
    dismiss={t('read.delete.no')}
    onconfirm={burn}
    onclose={dismissed}
  />
{/if}
