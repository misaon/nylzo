<script lang="ts">
  import type { Attachment } from 'svelte/attachments';

  import { createApi } from '../lib/api.ts';
  import { isRiskyFile, saveFile } from '../lib/download.ts';
  import { formatBytes, formatDuration, formatTime } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.ts';
  import { scryptInWorker } from '../lib/kdf.ts';
  import { createReader, type ReadView } from '../lib/read.ts';

  const { t, language } = getI18n();

  let view = $state.raw<ReadView>({ view: 'loading' });
  let passphrase = $state('');

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
    reader.reveal(passphrase);
  }
</script>

<section aria-labelledby="read-title" aria-busy={view.view === 'loading' || view.view === 'busy'}>
  <h2 id="read-title">{t('read.title')}</h2>

  {#if view.view === 'loading'}
    <p role="status">{t('read.loading')}</p>
  {:else if view.view === 'invalid'}
    <p role="alert">{t('read.invalid')}</p>
  {:else if view.view === 'unknown'}
    <p role="alert">{t('read.unknown')}</p>
  {:else if view.view === 'tampered'}
    <p role="alert">{t('read.tampered')}</p>
  {:else if view.view === 'failed'}
    <p role="alert">{t('read.failed')}</p>
    <button type="button" onclick={() => reader.load()}>{t('action.retry')}</button>
  {:else if view.view === 'retry_later'}
    <p role="alert">{t('read.retry_later', { time: formatDuration(view.retryAfter ?? 60, language) })}</p>
    <button type="button" onclick={() => reader.load()}>{t('action.retry')}</button>
  {:else if view.view === 'ended'}
    <p role="status">{t(`read.ended.${view.state}`)}</p>
    {#if view.firstOpenedAt}
      <p>{t('read.opened_at', { time: formatTime(view.firstOpenedAt, language) })}</p>
    {/if}
  {:else if view.view === 'ready'}
    <ul>
      {#if view.metadata.expiresAt}
        <li>{t('read.expires', { time: formatTime(view.metadata.expiresAt, language) })}</li>
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
    {#if view.rejected?.reason === 'malformed'}
      <p role="alert">{t('read.rejected.malformed')}</p>
    {:else if view.rejected?.locked}
      <p role="alert">{t('read.rejected.locked')}</p>
    {:else if view.rejected && view.rejected.retryAfter > 0}
      <p role="alert">{t('read.rejected.wait', { time: formatDuration(view.rejected.retryAfter, language) })}</p>
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
          required
          bind:value={passphrase}
          {@attach focus}
        />
      {/if}
      <p>{t('read.reveal.note')}</p>
      <button type="submit">{t('read.reveal')}</button>
    </form>
  {:else if view.view === 'busy'}
    <p role="status">{t(`busy.${view.step}`)}</p>
    {#if view.fraction === undefined}
      <progress aria-label={t(`busy.${view.step}`)}></progress>
    {:else}
      <progress aria-label={t(`busy.${view.step}`)} max="1" value={view.fraction}></progress>
    {/if}
  {:else if view.view === 'revealed'}
    <h3 tabindex="-1" {@attach focus}>{t('revealed.title')}</h3>
    {#if view.text !== undefined}
      <pre class="whitespace-pre-wrap break-words">{view.text}</pre>
    {/if}
    {#if view.file}
      {@const file = view.file}
      {#if isRiskyFile(file.name)}
        <p role="alert">{t('revealed.risky')}</p>
      {/if}
      <button type="button" onclick={() => saveFile(file.bytes, file.name)}>
        {t('revealed.download', { name: file.name, size: formatBytes(file.bytes.length, language) })}
      </button>
    {/if}
    <p>{t('revealed.note')}</p>
  {/if}
</section>
