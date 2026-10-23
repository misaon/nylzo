<script lang="ts">
  import { KDF_NONE, KDF_PBKDF2, KDF_SCRYPT, type KdfId } from '@nylzo/core';
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';

  import { createApi, type InstanceLimits } from '../lib/api.ts';
  import {
    CreateError,
    createMessage,
    passphraseIsWeak,
    validateCreate,
    type CreateFailure,
    type CreateField,
    type CreateProblem,
    type CreateProgress,
    type Created,
  } from '../lib/create.ts';
  import { failureKey, problemKey, progressKey, readAttachment, ttlChoices, ttlKey } from '../lib/create-form.ts';
  import { formatBytes, formatDuration, formatTime } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.ts';
  import { scryptInWorker, solvePowInWorker } from '../lib/kdf.ts';

  const { t, language } = getI18n();
  const api = createApi();

  let limits = $state.raw<InstanceLimits>();
  let loadFailed = $state(false);
  let text = $state('');
  let file = $state.raw<File>();
  let fileInput: HTMLInputElement | undefined;
  let form: HTMLFormElement | undefined;
  let ttlSeconds = $state(86400);
  let opensMax = $state(1);
  let passphrase = $state('');
  let kdfId = $state<KdfId>(KDF_SCRYPT);
  let problems = $state.raw<readonly CreateProblem[]>([]);
  let failure = $state.raw<CreateFailure>();
  let progress = $state.raw<CreateProgress>();
  let created = $state.raw<Created>();
  let usedPassphrase = $state(false);
  let copied = $state(false);
  let status = $state('');

  const choices = $derived(limits ? ttlChoices(limits) : []);
  const weak = $derived(passphrase !== '' && passphraseIsWeak(passphrase));
  const fieldMessages = $derived.by(() => {
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of problems) {
      messages[problem.field] ??= t(problemKey(problem), {
        size: formatBytes(problem.field === 'text' ? (limits?.maxTextBytes ?? 0) : (limits?.maxFileBytes ?? 0), language),
        max: limits?.maxOpens ?? 0,
      });
    }
    return messages;
  });

  async function loadLimits() {
    loadFailed = false;
    try {
      const instance = await api.instance();
      limits = instance.limits;
      ttlSeconds = instance.limits.defaultTtlSeconds;
      kdfId = instance.limits.defaultKdfId === KDF_NONE ? KDF_SCRYPT : instance.limits.defaultKdfId;
    } catch {
      loadFailed = true;
    }
  }

  loadLimits();

  const focus: Attachment<HTMLElement> = (element) => {
    element.focus();
  };

  const rememberForm: Attachment<HTMLFormElement> = (element) => {
    form = element;
    return () => {
      form = undefined;
    };
  };

  const rememberFileInput: Attachment<HTMLInputElement> = (element) => {
    fileInput = element;
    return () => {
      fileInput = undefined;
    };
  };

  function ttlLabel(seconds: number): string {
    const key = ttlKey(seconds);
    return key ? t(key) : formatDuration(seconds, language);
  }

  function failureText(value: CreateFailure): string {
    const retryAfter = value.kind === 'rate_limited' || value.kind === 'unavailable' ? value.retryAfter : undefined;
    return t(failureKey(value), { time: formatDuration(retryAfter ?? 60, language) });
  }

  function clearFile() {
    file = undefined;
    if (fileInput) {
      fileInput.value = '';
    }
  }

  async function showProblems(next: readonly CreateProblem[]) {
    problems = next;
    status = t('create.problems');
    await tick();
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }

  function onProgress(next: CreateProgress) {
    progress = next;
    status = t(progressKey(next));
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!limits || progress) {
      return;
    }
    failure = undefined;
    const attachment = await readAttachment(file, limits);
    if ('problem' in attachment) {
      await showProblems([attachment.problem]);
      return;
    }
    const input = { text, file: attachment.file, ttlSeconds, opensMax, passphrase, kdfId };
    const invalid = validateCreate(input, limits);
    if (invalid.length > 0) {
      await showProblems(invalid);
      return;
    }
    problems = [];
    try {
      created = await createMessage(
        input,
        {
          api,
          origin: location.origin,
          scrypt: (pass, saltPw, report) => scryptInWorker(pass, saltPw, { onProgress: report }),
          solvePow: (challenge, id, difficulty, deadline, report) => solvePowInWorker(challenge, id, difficulty, deadline, { onProgress: report }),
        },
        onProgress,
      );
      usedPassphrase = passphrase !== '';
      passphrase = '';
      text = '';
      clearFile();
      status = '';
    } catch (error) {
      if (error instanceof CreateError && error.failure.kind === 'invalid') {
        await showProblems(error.failure.problems);
      } else {
        failure = error instanceof CreateError ? error.failure : { kind: 'failed' };
        status = '';
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
    usedPassphrase = false;
  }
</script>

<p role="status" aria-live="polite">{status}</p>

{#if created}
  <section aria-labelledby="created-title">
    <h2 id="created-title" tabindex="-1" {@attach focus}>{t('created.title')}</h2>
    <label for="created-link">{t('created.link')}</label>
    <input id="created-link" type="text" readonly value={created.link} onfocus={(event) => event.currentTarget.select()} />
    <button type="button" onclick={copy}>{t('created.copy')}</button>
    <p>{copied ? t('created.copied') : ''}</p>
    <p>{t('created.expires', { time: formatTime(created.expiresAt, language) })}</p>
    {#if usedPassphrase}
      <p>{t('created.passphrase')}</p>
    {/if}
    <button type="button" onclick={another}>{t('created.another')}</button>
  </section>
{:else}
  <section aria-labelledby="create-title">
    <h2 id="create-title">{t('create.title')}</h2>
    {#if loadFailed}
      <p role="alert">{t('failure.instance')}</p>
      <button type="button" onclick={loadLimits}>{t('action.retry')}</button>
    {:else if !limits}
      <p>{t('create.loading')}</p>
    {:else}
      <form onsubmit={submit} novalidate {@attach rememberForm}>
        <label for="create-text">{t('create.text')}</label>
        <textarea
          id="create-text"
          spellcheck="false"
          autocapitalize="off"
          autocomplete="off"
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
        <select id="create-ttl" bind:value={ttlSeconds} aria-invalid={fieldMessages.ttl !== undefined} aria-describedby="create-ttl-problem">
          {#each choices as seconds (seconds)}
            <option value={seconds}>{ttlLabel(seconds)}</option>
          {/each}
        </select>
        <p id="create-ttl-problem">{fieldMessages.ttl ?? ''}</p>

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
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          data-1p-ignore=""
          data-lpignore="true"
          data-bwignore="true"
          data-form-type="other"
          bind:value={passphrase}
          aria-invalid={fieldMessages.passphrase !== undefined}
          aria-describedby="create-passphrase-hint create-passphrase-problem"
        />
        <p id="create-passphrase-hint">{t('create.passphrase.hint')}</p>
        <p id="create-passphrase-problem">{fieldMessages.passphrase ?? (weak ? t('create.passphrase.weak') : '')}</p>

        {#if passphrase !== ''}
          <fieldset>
            <legend>{t('create.kdf')}</legend>
            <label><input type="radio" name="kdf" value={KDF_SCRYPT} bind:group={kdfId} /> {t('create.kdf.1')}</label>
            <label><input type="radio" name="kdf" value={KDF_PBKDF2} bind:group={kdfId} /> {t('create.kdf.2')}</label>
          </fieldset>
        {/if}

        <button type="submit" disabled={progress !== undefined}>{t('create.submit')}</button>
      </form>
      {#if failure}
        <p role="alert">{failureText(failure)}</p>
      {/if}
      {#if progress}
        {#if (progress.step === 'deriving' || progress.step === 'proving') && progress.fraction !== undefined}
          <progress aria-label={t(progressKey(progress))} max="1" value={progress.fraction}></progress>
        {:else}
          <progress aria-label={t(progressKey(progress))}></progress>
        {/if}
      {/if}
    {/if}
  </section>
{/if}
