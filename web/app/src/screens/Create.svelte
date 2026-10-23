<script lang="ts">
  import { KDF_NONE, KDF_PBKDF2, KDF_SCRYPT, MIN_PASSPHRASE_CHARS, type KdfId } from '@nylzo/core';
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';

  import { createApi, type InstanceLimits } from '../lib/api.ts';
  import {
    CreateError,
    WEAK_BELOW_CHARS,
    WEAK_BELOW_WORDS,
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
  import { formatBytes, formatDuration, formatTime, formatWait } from '../lib/format.ts';
  import { getI18n } from '../lib/i18n.svelte.ts';
  import { scryptInWorker, solvePowInWorker } from '../lib/kdf.ts';
  import { indexedDbSentStore, rememberSent } from '../lib/sent.ts';

  type Shown = Pick<Created, 'link' | 'expiresAt'>;

  const SAVE_TIMEOUT_MS = 5000;

  const i18n = getI18n();
  const { t } = i18n;
  const api = createApi();
  const sentStore = indexedDbSentStore();

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
  let submitting = $state(false);
  let created = $state.raw<Shown>();
  let remember = $state(true);
  let remembered = $state<'saving' | 'remembered' | 'failed' | 'off'>('off');
  let usedPassphrase = $state(false);
  let copied = $state(false);
  let showingProblems = $state(false);

  const status = $derived(progress ? t(progressKey(progress)) : showingProblems ? t('create.problems') : '');
  const rememberedText = $derived(remembered === 'remembered' ? t('created.remembered') : remembered === 'failed' ? t('created.not_remembered') : '');
  const choices = $derived(limits ? ttlChoices(limits) : []);
  const weak = $derived(passphrase !== '' && passphraseIsWeak(passphrase));
  const fieldMessages = $derived.by(() => {
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of problems) {
      messages[problem.field] ??= t(problemKey(problem), {
        size: formatBytes(problem.field === 'text' ? (limits?.maxTextBytes ?? 0) : (limits?.maxFileBytes ?? 0), i18n.language),
        max: limits?.maxOpens ?? 0,
        min: MIN_PASSPHRASE_CHARS,
      });
    }
    return messages;
  });
  const passphraseProblem = $derived(
    fieldMessages.passphrase ?? (weak ? t('create.passphrase.weak', { words: WEAK_BELOW_WORDS, chars: WEAK_BELOW_CHARS }) : ''),
  );

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
    return key ? t(key) : formatDuration(seconds, i18n.language);
  }

  function failureText(value: CreateFailure): string {
    const retryAfter = value.kind === 'rate_limited' || value.kind === 'unavailable' ? value.retryAfter : undefined;
    return t(failureKey(value), { time: formatWait(retryAfter, i18n.language) });
  }

  function clearFile() {
    file = undefined;
    if (fileInput) {
      fileInput.value = '';
    }
  }

  async function showProblems(next: readonly CreateProblem[]) {
    problems = next;
    showingProblems = true;
    await tick();
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }

  function onProgress(next: CreateProgress) {
    progress = next;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!limits || submitting) {
      return;
    }
    submitting = true;
    try {
      await send(limits);
    } finally {
      submitting = false;
      progress = undefined;
    }
  }

  async function send(allowed: InstanceLimits) {
    failure = undefined;
    showingProblems = false;
    const attachment = await readAttachment(file, allowed);
    if ('problem' in attachment) {
      await showProblems([attachment.problem]);
      return;
    }
    const input = { text, file: attachment.file, ttlSeconds, opensMax, passphrase, kdfId };
    const invalid = validateCreate(input, allowed);
    if (invalid.length > 0) {
      await showProblems(invalid);
      return;
    }
    problems = [];
    try {
      const result = await createMessage(
        input,
        {
          api,
          origin: location.origin,
          scrypt: (pass, saltPw, report) => scryptInWorker(pass, saltPw, { onProgress: report }),
          solvePow: (challenge, id, difficulty, deadline, report) => solvePowInWorker(challenge, id, difficulty, deadline, { onProgress: report }),
        },
        onProgress,
      );
      const shown = { link: result.link, expiresAt: result.expiresAt };
      created = shown;
      remembered = remember ? 'saving' : 'off';
      if (remember) {
        rememberMessage(result, shown);
      }
      usedPassphrase = passphrase !== '';
      passphrase = '';
      text = '';
      clearFile();
    } catch (error) {
      if (error instanceof CreateError && error.failure.kind === 'invalid') {
        await showProblems(error.failure.problems);
      } else {
        failure = error instanceof CreateError ? error.failure : { kind: 'failed' };
      }
    }
  }

  async function rememberMessage(result: Created, shown: Shown) {
    const expiresAt = Date.parse(result.expiresAt);
    const record = { id: result.id, createdAt: Math.min(Date.now(), expiresAt), expiresAt, manageKey: result.manageKey };
    const saved = await rememberSent(sentStore, record, SAVE_TIMEOUT_MS);
    if (created === shown) {
      remembered = saved ? 'remembered' : 'failed';
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
    remembered = 'off';
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
    <p>{t('created.expires', { time: formatTime(created.expiresAt, i18n.language) })}</p>
    {#if usedPassphrase}
      <p>{t('created.passphrase')}</p>
    {/if}
    {#if remembered !== 'off'}
      <p role="status">{rememberedText}</p>
      <a href="/sent">{t('created.status_link')}</a>
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
          autocorrect="off"
          autocapitalize="off"
          autocomplete="off"
          bind:value={text}
          aria-invalid={fieldMessages.content !== undefined || fieldMessages.text !== undefined}
          aria-describedby="create-text-problem"
        ></textarea>
        <p id="create-text-problem" data-problem>{fieldMessages.content ?? fieldMessages.text ?? ''}</p>

        <label for="create-file">{t('create.file', { size: formatBytes(limits.maxFileBytes, i18n.language) })}</label>
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
        <p id="create-file-problem" data-problem>{fieldMessages.file ?? ''}</p>

        <label for="create-ttl">{t('create.ttl')}</label>
        <select id="create-ttl" bind:value={ttlSeconds} aria-invalid={fieldMessages.ttl !== undefined} aria-describedby="create-ttl-problem">
          {#each choices as seconds (seconds)}
            <option value={seconds}>{ttlLabel(seconds)}</option>
          {/each}
        </select>
        <p id="create-ttl-problem" data-problem>{fieldMessages.ttl ?? ''}</p>

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
        <p id="create-opens-problem" data-problem>{fieldMessages.opens ?? ''}</p>

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
        <p id="create-passphrase-hint">{t('create.passphrase.hint', { min: MIN_PASSPHRASE_CHARS })}</p>
        <p id="create-passphrase-problem" data-problem>{passphraseProblem}</p>

        {#if passphrase !== ''}
          <fieldset>
            <legend>{t('create.kdf')}</legend>
            <label><input type="radio" name="kdf" value={KDF_SCRYPT} bind:group={kdfId} /> {t('create.kdf.1')}</label>
            <label><input type="radio" name="kdf" value={KDF_PBKDF2} bind:group={kdfId} /> {t('create.kdf.2')}</label>
          </fieldset>
        {/if}

        <label><input type="checkbox" bind:checked={remember} /> {t('create.remember')}</label>

        <button type="submit" disabled={submitting}>{t('create.submit')}</button>
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
