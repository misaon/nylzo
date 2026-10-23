<script lang="ts">
  import { KDF_NONE, KDF_PBKDF2, KDF_SCRYPT, type KdfId } from '@nylzo/core';
  import type { Attachment } from 'svelte/attachments';

  import { createApi, type InstanceLimits } from '../lib/api.ts';
  import {
    CreateError,
    createMessage,
    passphraseIsWeak,
    validateCreate,
    type CreateFailure,
    type CreateField,
    type CreateInput,
    type CreateProblem,
    type CreateProgress,
    type Created,
  } from '../lib/create.ts';
  import { formatBytes, formatDuration, formatTime } from '../lib/format.ts';
  import { getI18n, type MessageKey } from '../lib/i18n.ts';
  import { scryptInWorker, solvePowInWorker } from '../lib/kdf.ts';

  const TTL_CHOICES = [3600, 86400, 259200, 604800] as const;

  const { t, language } = getI18n();
  const api = createApi();

  let limits = $state.raw<InstanceLimits>();
  let loadFailed = $state(false);
  let text = $state('');
  let file = $state.raw<File>();
  let fileInput: HTMLInputElement | undefined;
  let ttlSeconds = $state(86400);
  let opensMax = $state(1);
  let passphrase = $state('');
  let kdfId = $state<KdfId>(KDF_PBKDF2);
  let problems = $state.raw<readonly CreateProblem[]>([]);
  let failure = $state.raw<CreateFailure>();
  let progress = $state.raw<CreateProgress>();
  let created = $state.raw<Created>();
  let copied = $state(false);

  const ttlChoices = $derived.by(() => {
    const maxTtl = limits?.maxTtlSeconds ?? 0;
    return TTL_CHOICES.filter((seconds) => seconds <= maxTtl);
  });
  const weak = $derived(passphrase !== '' && passphraseIsWeak(passphrase));
  const fieldMessages = $derived.by(() => {
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of problems) {
      messages[problem.field] ??= problemText(problem);
    }
    return messages;
  });

  async function loadLimits() {
    loadFailed = false;
    try {
      const instance = await api.instance();
      limits = instance.limits;
      ttlSeconds = instance.limits.defaultTtlSeconds;
      kdfId = instance.limits.defaultKdfId === KDF_NONE ? KDF_PBKDF2 : instance.limits.defaultKdfId;
    } catch {
      loadFailed = true;
    }
  }

  loadLimits();

  const focus: Attachment<HTMLElement> = (element) => {
    element.focus();
  };

  const rememberFileInput: Attachment<HTMLInputElement> = (element) => {
    fileInput = element;
    return () => {
      fileInput = undefined;
    };
  };

  function problemText(problem: CreateProblem): string {
    const key = `problem.${problem.field}.${problem.code}` as MessageKey;
    return t(key, {
      size: formatBytes(problem.field === 'text' ? (limits?.maxTextBytes ?? 0) : (limits?.maxFileBytes ?? 0), language),
      max: limits?.maxOpens ?? 0,
    });
  }

  function failureText(value: CreateFailure): string {
    if (value.kind === 'rate_limited' || value.kind === 'unavailable') {
      return t(`failure.${value.kind}`, { time: formatDuration(value.retryAfter ?? 60, language) });
    }
    return value.kind === 'invalid' ? t('failure.failed') : t(`failure.${value.kind}`);
  }

  function progressText(value: CreateProgress): string {
    return t(`progress.${value.step}`);
  }

  function clearFile() {
    file = undefined;
    if (fileInput) {
      fileInput.value = '';
    }
  }

  async function readInput(current: InstanceLimits): Promise<CreateInput | undefined> {
    const base = { text, ttlSeconds, opensMax, passphrase, kdfId };
    if (file && file.size > current.maxFileBytes) {
      problems = [{ field: 'file', code: 'too_large' }];
      return undefined;
    }
    const attached = file && { name: file.name, mime: file.type, bytes: new Uint8Array(await file.arrayBuffer()) };
    const input: CreateInput = { ...base, file: attached };
    problems = validateCreate(input, current);
    return problems.length === 0 ? input : undefined;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!limits || progress) {
      return;
    }
    failure = undefined;
    progress = { step: 'preparing' };
    try {
      const input = await readInput(limits);
      if (!input) {
        return;
      }
      created = await createMessage(
        input,
        {
          api,
          origin: location.origin,
          scrypt: (pass, saltPw, onProgress) => scryptInWorker(pass, saltPw, { onProgress }),
          solvePow: (challenge, id, difficulty, onProgress) => solvePowInWorker(challenge, id, difficulty, { onProgress }),
        },
        (next) => {
          progress = next;
        },
      );
    } catch (error) {
      if (error instanceof CreateError && error.failure.kind === 'invalid') {
        problems = error.failure.problems;
      } else {
        failure = error instanceof CreateError ? error.failure : { kind: 'failed' };
      }
    } finally {
      progress = undefined;
    }
  }

  async function copy() {
    if (!created) {
      return;
    }
    try {
      await navigator.clipboard.writeText(created.link);
      copied = true;
    } catch {
      copied = false;
    }
  }

  function another() {
    created = undefined;
    copied = false;
    text = '';
    passphrase = '';
    clearFile();
  }
</script>

{#if created}
  <section aria-labelledby="created-title">
    <h2 id="created-title" tabindex="-1" {@attach focus}>{t('created.title')}</h2>
    <label for="created-link">{t('created.link')}</label>
    <input id="created-link" type="text" readonly value={created.link} onfocus={(event) => event.currentTarget.select()} />
    <button type="button" onclick={copy}>{t('created.copy')}</button>
    <p role="status">{copied ? t('created.copied') : ''}</p>
    <p>{t('created.expires', { time: formatTime(created.expiresAt, language) })}</p>
    {#if passphrase !== ''}
      <p>{t('created.passphrase')}</p>
    {/if}
    <button type="button" onclick={another}>{t('created.another')}</button>
  </section>
{:else}
  <section aria-labelledby="create-title" aria-busy={progress !== undefined}>
    <h2 id="create-title">{t('create.title')}</h2>
    {#if loadFailed}
      <p role="alert">{t('failure.instance')}</p>
      <button type="button" onclick={loadLimits}>{t('action.retry')}</button>
    {:else if !limits}
      <p role="status">{t('create.loading')}</p>
    {:else}
      <form onsubmit={submit} novalidate>
        <label for="create-text">{t('create.text')}</label>
        <textarea
          id="create-text"
          bind:value={text}
          aria-invalid={fieldMessages.content !== undefined || fieldMessages.text !== undefined}
          aria-describedby="create-text-problem"
        ></textarea>
        <p id="create-text-problem">{fieldMessages.content ?? fieldMessages.text ?? ''}</p>

        <label for="create-file">{t('create.file', { size: formatBytes(limits.maxFileBytes, language) })}</label>
        <input
          id="create-file"
          type="file"
          {@attach rememberFileInput}
          onchange={(event) => (file = event.currentTarget.files?.[0])}
          aria-invalid={fieldMessages.file !== undefined}
          aria-describedby="create-file-problem"
        />
        {#if file}
          <button type="button" onclick={clearFile}>{t('create.file.clear')}</button>
        {/if}
        <p id="create-file-problem">{fieldMessages.file ?? ''}</p>

        <label for="create-ttl">{t('create.ttl')}</label>
        <select id="create-ttl" bind:value={ttlSeconds}>
          {#each ttlChoices as seconds (seconds)}
            <option value={seconds}>{t(`ttl.${seconds}`)}</option>
          {/each}
        </select>

        <label for="create-opens">{t('create.opens', { max: limits.maxOpens })}</label>
        <input
          id="create-opens"
          type="number"
          min="1"
          max={limits.maxOpens}
          bind:value={opensMax}
          aria-invalid={fieldMessages.opens !== undefined}
          aria-describedby="create-opens-problem"
        />
        <p id="create-opens-problem">{fieldMessages.opens ?? ''}</p>

        <label for="create-passphrase">{t('create.passphrase')}</label>
        <input
          id="create-passphrase"
          type="password"
          autocomplete="new-password"
          bind:value={passphrase}
          aria-invalid={fieldMessages.passphrase !== undefined}
          aria-describedby="create-passphrase-hint create-passphrase-problem"
        />
        <p id="create-passphrase-hint">{t('create.passphrase.hint')}</p>
        <p id="create-passphrase-problem">{fieldMessages.passphrase ?? (weak ? t('create.passphrase.weak') : '')}</p>

        {#if passphrase !== ''}
          <fieldset>
            <legend>{t('create.kdf')}</legend>
            <label><input type="radio" name="kdf" value={KDF_PBKDF2} bind:group={kdfId} /> {t('create.kdf.2')}</label>
            <label><input type="radio" name="kdf" value={KDF_SCRYPT} bind:group={kdfId} /> {t('create.kdf.1')}</label>
          </fieldset>
        {/if}

        <button type="submit" disabled={progress !== undefined}>{t('create.submit')}</button>
      </form>
      {#if failure}
        <p role="alert">{failureText(failure)}</p>
      {/if}
      {#if progress}
        <p role="status">{progressText(progress)}</p>
        {#if progress.step === 'deriving' || progress.step === 'proving'}
          <progress aria-label={progressText(progress)} max="1" value={progress.fraction}></progress>
        {:else}
          <progress aria-label={progressText(progress)}></progress>
        {/if}
      {/if}
    {/if}
  </section>
{/if}
