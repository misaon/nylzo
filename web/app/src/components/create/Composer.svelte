<script lang="ts">
  import { MAX_FILE_BYTES, MAX_TEXT_BYTES, MIN_PASSPHRASE_CHARS } from '@nylzo/core';
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { prefersReducedMotion } from 'svelte/motion';

  import { createApi, type InstanceLimits } from '../../lib/api.ts';
  import {
    CreateError,
    WEAK_BELOW_CHARS,
    WEAK_BELOW_WORDS,
    createMessage,
    passphraseIsWeak,
    validateCreate,
    type CreateFailure,
    type CreateField,
    type CreateInput,
    type CreateProblem,
    type CreateProgress,
    type Created,
  } from '../../lib/create.ts';
  import { failureKey, problemKey, readAttachment, ttlChoices, ttlKey } from '../../lib/create-form.ts';
  import { formatBytes, formatDuration, formatSize, formatWait } from '../../lib/format.ts';
  import { getI18n } from '../../lib/i18n.svelte.ts';
  import { scryptInWorker, solvePowInWorker } from '../../lib/kdf.ts';
  import { pace } from '../../lib/pacing.ts';
  import type { PipelineNode } from '../../lib/pipeline.ts';
  import { indexedDbSentStore, rememberSent } from '../../lib/sent.ts';
  import Icon from '../ui/Icon.svelte';
  import Pipeline from '../ui/Pipeline.svelte';
  import Popover from '../ui/Popover.svelte';
  import Switch from '../ui/Switch.svelte';
  import CreatedPanel from './CreatedPanel.svelte';

  type Menu = 'ttl' | 'opens' | 'security';
  type Phase = 'compose' | 'encrypting' | 'done';

  interface Sealed {
    readonly created: Created;
    readonly opensMax: number;
    readonly passphrase: boolean;
    readonly saved?: Promise<boolean> | undefined;
  }

  const REACHED: Record<CreateProgress['step'], number> = { preparing: 0, deriving: 1, encrypting: 2, proving: 3, uploading: 3 };
  const MIN_MS = 5000;
  const REDUCED_MIN_MS = 1200;
  const TICK_MS = 60;
  const SAVE_TIMEOUT_MS = 5000;
  const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

  const i18n = getI18n();
  const { t } = i18n;
  const api = createApi();
  const sentStore = indexedDbSentStore();
  const uid = $props.id();
  const encoder = new TextEncoder();

  let limits = $state.raw<InstanceLimits>();
  let loadFailed = $state(false);
  let text = $state('');
  let file = $state.raw<File>();
  let dragging = $state(false);
  let ttlSeconds = $state(86400);
  let opensMax = $state(1);
  let protect = $state(false);
  let passphrase = $state('');
  let remember = $state(true);
  let menu = $state<Menu | null>(null);
  let problems = $state.raw<readonly CreateProblem[]>([]);
  let failure = $state.raw<CreateFailure>();
  let phase = $state<Phase>('compose');
  let reached = $state(0);
  let fraction = $state<number | undefined>();
  let proving = $state(false);
  let elapsed = $state(0);
  let cipher = $state('');
  let sealed = $state.raw<Sealed>();
  let sealingWithPassphrase = $state(false);
  let remembered = $state<'off' | 'saving' | 'remembered' | 'failed'>('off');
  let pending: Sealed | undefined;
  let run = 0;
  let startedAt = 0;
  let timer: ReturnType<typeof setInterval> | undefined;

  const usesPassphrase = $derived(phase === 'compose' ? protect && passphrase !== '' : sealingWithPassphrase);
  const nodes: readonly PipelineNode[] = $derived([
    { id: 'key', label: t('step.key.label'), icon: 'key', title: t('step.key.title'), detail: t('step.key.detail') },
    {
      id: 'derive',
      label: t('step.derive.label'),
      icon: 'fork',
      title: t('step.derive.title'),
      detail: t(usesPassphrase ? 'step.derive.detail.passphrase' : 'step.derive.detail'),
    },
    { id: 'encrypt', label: t('step.encrypt.label'), icon: 'lock', title: t('step.encrypt.title'), detail: t('step.encrypt.detail') },
    {
      id: 'send',
      label: t('step.send.label'),
      icon: 'upload',
      title: t(proving ? 'step.send.title.pow' : 'step.send.title'),
      detail: t(proving ? 'step.send.detail.pow' : 'step.send.detail'),
    },
    { id: 'link', label: t('step.link.label'), icon: 'link', title: t('step.link.title'), detail: t('step.link.detail') },
  ]);
  const minMs = $derived(prefersReducedMotion.current ? REDUCED_MIN_MS : MIN_MS);
  const shown = $derived(pace({ elapsedMs: elapsed, minMs, steps: nodes.length, reached, fraction }));
  const textBytes = $derived(encoder.encode(text).length);
  const textLimit = $derived(Math.min(limits?.maxTextBytes ?? MAX_TEXT_BYTES, MAX_TEXT_BYTES));
  const fileLimit = $derived(limits?.maxFileBytes ?? MAX_FILE_BYTES);
  const choices = $derived(limits ? ttlChoices(limits) : [ttlSeconds]);
  const maxOpens = $derived(limits?.maxOpens ?? 1);
  const weak = $derived(protect && passphrase !== '' && passphraseIsWeak(passphrase));
  const problemOf = $derived.by(() => {
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of problems) {
      messages[problem.field] ??= t(problemKey(problem), {
        size: formatBytes(problem.field === 'text' ? textLimit : fileLimit, i18n.language),
        max: maxOpens,
        min: MIN_PASSPHRASE_CHARS,
      });
    }
    return messages;
  });
  const passphraseMessage = $derived(problemOf.passphrase ?? (weak ? t('create.passphrase.weak', { words: WEAK_BELOW_WORDS, chars: WEAK_BELOW_CHARS }) : ''));
  const notice = $derived(
    loadFailed
      ? t('failure.instance')
      : failure
        ? failureText(failure)
        : (problemOf.content ?? problemOf.text ?? problemOf.file ?? problemOf.ttl ?? problemOf.opens ?? problemOf.passphrase ?? ''),
  );
  const busy = $derived(phase !== 'compose');
  let submitting = false;

  const stepTitle = $derived(busy && phase === 'encrypting' ? (nodes[shown.step]?.title ?? '') : '');

  async function loadLimits() {
    loadFailed = false;
    try {
      const instance = await api.instance();
      limits = instance.limits;
      ttlSeconds = instance.limits.defaultTtlSeconds;
      opensMax = Math.min(opensMax, instance.limits.maxOpens);
    } catch {
      loadFailed = true;
    }
  }

  function failureText(value: CreateFailure): string {
    const retryAfter = value.kind === 'rate_limited' || value.kind === 'unavailable' ? value.retryAfter : undefined;
    return t(failureKey(value), { time: formatWait(retryAfter, i18n.language) });
  }

  function ttlLabel(seconds: number): string {
    const key = ttlKey(seconds);
    return key ? t(key) : formatDuration(seconds, i18n.language);
  }

  function opensLabel(count: number): string {
    return t(count === 1 ? 'compose.opens.one' : 'compose.opens.other', { count });
  }

  function toggle(next: Menu) {
    menu = menu === next ? null : next;
  }

  function closeMenu(trigger: Menu) {
    menu = null;
    document.getElementById(`${uid}-${trigger}-trigger`)?.focus();
  }

  function onwindowclick(event: MouseEvent) {
    if (menu && !(event.target instanceof Element && event.target.closest('[data-menu]'))) {
      menu = null;
    }
  }

  function scramble(plain: string, progress: number): string {
    const cut = Math.floor(plain.length * Math.min(1, progress * 1.3));
    let out = '';
    for (let index = 0; index < plain.length; index++) {
      out += index < cut ? randomChar() : plain[index];
    }
    const tail = Math.floor(progress * 1100);
    for (let index = 0; index < tail; index++) {
      out += randomChar();
    }
    return out;
  }

  function randomChar(): string {
    return ALPHABET[Math.floor(Math.random() * ALPHABET.length)] ?? 'A';
  }

  function pickFile(next: File | undefined) {
    file = next;
    problems = problems.filter((problem) => problem.field !== 'file' && problem.field !== 'content');
    if (next && limits && next.size > limits.maxFileBytes) {
      problems = [...problems, { field: 'file', code: 'too_large' }];
    }
  }

  function field(name: 'text' | 'passphrase' | 'file'): HTMLElement | null {
    return document.getElementById(`${uid}-${name}`);
  }

  function removeFile() {
    pickFile(undefined);
    const input = field('file');
    if (input instanceof HTMLInputElement) {
      input.value = '';
    }
  }

  function ondragover(event: DragEvent) {
    if (busy || !event.dataTransfer?.types.includes('Files')) {
      return;
    }
    event.preventDefault();
    dragging = true;
  }

  function ondragleave(event: DragEvent) {
    if (!(event.relatedTarget instanceof Node && event.currentTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) {
      dragging = false;
    }
  }

  function ondrop(event: DragEvent) {
    if (!event.dataTransfer?.types.includes('Files')) {
      return;
    }
    event.preventDefault();
    dragging = false;
    if (!busy) {
      pickFile(event.dataTransfer?.files[0]);
    }
  }

  function toggleProtect() {
    protect = !protect;
    if (!protect) {
      passphrase = '';
      problems = problems.filter((problem) => problem.field !== 'passphrase');
    } else {
      tick().then(() => field('passphrase')?.focus());
    }
  }

  async function showProblems(next: readonly CreateProblem[]) {
    problems = next;
    await tick();
    if (next.some((problem) => problem.field === 'passphrase')) {
      menu = 'security';
      await tick();
      field('passphrase')?.focus();
    } else if (next.some((problem) => problem.field === 'content' || problem.field === 'text')) {
      field('text')?.focus();
    } else if (next.some((problem) => problem.field === 'file')) {
      field('file')?.focus();
    }
  }

  function onProgress(progress: CreateProgress) {
    proving = progress.step === 'proving';
    reached = Math.max(reached, REACHED[progress.step]);
    fraction = 'fraction' in progress ? progress.fraction : undefined;
  }

  function startTimeline(source: string) {
    const plain = source.replace(/\s+/g, ' ').slice(0, 140);
    startedAt = performance.now();
    elapsed = 0;
    reached = 0;
    fraction = undefined;
    proving = false;
    cipher = scramble(plain, 0);
    phase = 'encrypting';
    clearInterval(timer);
    timer = setInterval(() => {
      elapsed = performance.now() - startedAt;
      cipher = scramble(plain, shown.progress);
      if (shown.finished && pending) {
        finish(pending);
      }
    }, TICK_MS);
  }

  function stopTimeline() {
    clearInterval(timer);
    timer = undefined;
  }

  function finish(result: Sealed) {
    stopTimeline();
    pending = undefined;
    sealed = result;
    phase = 'done';
    remembered = result.saved ? 'saving' : 'off';
    result.saved?.then((saved) => {
      if (sealed === result) {
        remembered = saved ? 'remembered' : 'failed';
      }
    });
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!limits || busy || submitting) {
      return;
    }
    submitting = true;
    try {
      await send(limits);
    } finally {
      submitting = false;
    }
  }

  async function send(allowed: InstanceLimits) {
    const current = run;
    const stale = () => current !== run;
    menu = null;
    failure = undefined;
    const attachment = await readAttachment(file, allowed);
    if (stale()) {
      return;
    }
    if ('problem' in attachment) {
      await showProblems([attachment.problem]);
      return;
    }
    const input: CreateInput = { text, file: attachment.file, ttlSeconds, opensMax, passphrase: protect ? passphrase : '' };
    const unset: CreateProblem[] = protect && passphrase === '' ? [{ field: 'passphrase', code: 'too_short' }] : [];
    const invalid = [...unset, ...validateCreate(input, allowed)];
    if (invalid.length > 0) {
      await showProblems(invalid);
      return;
    }
    problems = [];
    sealingWithPassphrase = input.passphrase !== '';
    startTimeline(text.trim() === '' ? (file?.name ?? '') : text);
    try {
      const created = await createMessage(
        input,
        {
          api,
          origin: location.origin,
          scrypt: (pass, saltPw, report) => scryptInWorker(pass, saltPw, { onProgress: report }),
          solvePow: (challenge, id, difficulty, deadline, report) => solvePowInWorker(challenge, id, difficulty, deadline, { onProgress: report }),
        },
        (progress) => {
          if (!stale()) {
            onProgress(progress);
          }
        },
      );
      if (stale()) {
        return;
      }
      const saved = remember ? rememberMessage(created) : undefined;
      text = '';
      passphrase = '';
      removeFile();
      pending = { created, opensMax, passphrase: input.passphrase !== '', saved };
      reached = nodes.length;
      fraction = undefined;
    } catch (error) {
      if (stale()) {
        return;
      }
      stopTimeline();
      phase = 'compose';
      if (error instanceof CreateError && error.failure.kind === 'invalid') {
        await showProblems(error.failure.problems);
      } else {
        failure = error instanceof CreateError ? error.failure : { kind: 'failed' };
        tick().then(() => document.getElementById(`${uid}-submit`)?.focus());
      }
    }
  }

  function reset() {
    sealed = undefined;
    remembered = 'off';
    text = '';
    passphrase = '';
    protect = false;
    removeFile();
    phase = 'compose';
  }

  function another() {
    reset();
    tick().then(() => field('text')?.focus());
  }

  function leave() {
    run++;
    stopTimeline();
    pending = undefined;
    cipher = '';
    menu = null;
    reset();
  }

  function returned(event: PageTransitionEvent) {
    if (event.persisted) {
      location.reload();
    }
  }

  async function rememberMessage(created: Created): Promise<boolean> {
    const expiresAt = Date.parse(created.expiresAt);
    const record = { id: created.id, createdAt: Math.min(Date.now(), expiresAt), expiresAt, manageKey: created.manageKey };
    return rememberSent(sentStore, record, SAVE_TIMEOUT_MS);
  }

  const cleanup: Attachment = () => {
    loadLimits();
    return () => clearInterval(timer);
  };

  const chipBase = 'flex h-11 w-full items-center justify-center gap-2 rounded-[9px] px-3 text-[13px] whitespace-nowrap ring-1 sm:w-auto sm:justify-start md:h-9';
  const chipIdle = 'bg-white/4 text-ink-soft ring-white/10 hover:bg-white/8 hover:text-ink';
  const chipOpen = 'bg-white/10 text-ink ring-white/25';
  const chipAccent = 'bg-aegis-500/15 text-aegis-200 ring-aegis-400/40 hover:bg-aegis-500/20';
  const chipProblem = 'bg-rose-500/10 text-rose-200 ring-rose-300/50';
</script>

<svelte:window onclick={onwindowclick} onpagehide={leave} onpageshow={returned} />

<section id="napsat" class="px-4 pt-20 pb-32 sm:px-6 md:pt-28 md:pb-40" aria-labelledby="{uid}-title" {@attach cleanup}>
  <div class="mx-auto max-w-[860px] text-center">
    <h1 class="animate-rise-visible font-display text-[36px] leading-[1.12] font-light tracking-[-0.025em] text-balance sm:text-[50px] lg:text-[62px]">
      {t('hero.title.lead')} <span class="text-aegis-300">{t('hero.title.accent')}</span>
    </h1>
    <p class="mx-auto mt-6 max-w-[640px] animate-rise-visible text-base leading-[1.75] text-balance text-ink-soft [animation-delay:120ms] sm:text-[17px]">
      {t('hero.intro')}
    </p>
  </div>

  <div class="relative z-20 mx-auto mt-14 max-w-[860px] animate-rise [animation-delay:240ms]">
    <div class="glow-under" aria-hidden="true"></div>
    <div class="glass rounded-card">
      <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
      <h2 id="{uid}-title" class="sr-only">{t('create.title')}</h2>

      <div class="stack relative">
        <form
          class={['fade-swap flex flex-col text-left', phase === 'done' ? 'pointer-events-none translate-y-3 opacity-0' : 'translate-y-0 opacity-100']}
          inert={phase === 'done'}
          onsubmit={submit}
          novalidate
        >
          <div class="group relative" role="presentation" {ondragover} ondragenter={ondragover} {ondragleave} {ondrop}>
            <label for="{uid}-text" class="sr-only">{t('create.text')}</label>
            <textarea
              id="{uid}-text"
              bind:value={text}
              rows="7"
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              autocorrect="off"
              readonly={busy}
              placeholder={t('compose.placeholder')}
              aria-invalid={problemOf.content !== undefined || problemOf.text !== undefined}
              aria-describedby="{uid}-notice"
              class={[
                'block min-h-[200px] w-full resize-none rounded-t-card bg-transparent px-6 pt-6 pb-14 text-base leading-[1.7] text-ink caret-aegis-300 outline-none transition-opacity duration-500 placeholder:text-ink-mute md:min-h-[280px] md:text-[17px]',
                phase === 'encrypting' ? 'opacity-0' : 'opacity-100',
              ]}
            ></textarea>
            <div class="pointer-events-none absolute inset-x-4 bottom-3 flex items-center justify-between gap-3">
              <span
                class={[
                  'flex h-8 min-w-0 origin-left items-center gap-2 rounded-[8px] pr-0.5 pl-2.5 text-[13px] ring-1 transition-[opacity,translate,scale] duration-500 ease-silk',
                  problemOf.file ? 'bg-rose-500/10 ring-rose-300/50' : 'bg-nebula-400/10 ring-nebula-300/30',
                  file && phase === 'compose' ? 'pointer-events-auto translate-y-0 scale-100 opacity-100' : 'translate-y-1 scale-95 opacity-0',
                ]}
                inert={!file || phase !== 'compose'}
              >
                <Icon name="file" class="size-4 shrink-0 text-nebula-300" />
                <span class="truncate text-ink">{file?.name ?? ''}</span>
                <span class="shrink-0 font-mono text-[11px] text-ink-mute">{file ? formatBytes(file.size, i18n.language) : ''}</span>
                <button
                  type="button"
                  onclick={removeFile}
                  aria-label={t('create.file.clear')}
                  class="grid size-7 shrink-0 place-items-center rounded-[6px] text-ink-soft hover:bg-white/8 hover:text-ink max-md:-my-1.5 max-md:size-11"
                >
                  <Icon name="close" class="size-3.5" stroke={2} />
                </button>
              </span>
              <span
                class={[
                  'shrink-0 pr-2 font-mono text-[11px] tabular-nums transition-opacity duration-500',
                  problemOf.text ? 'text-rose-300' : 'text-ink-mute',
                  phase === 'compose' ? 'opacity-100' : 'opacity-0',
                ]}
                aria-hidden="true"
              >
                {t('compose.counter', { size: formatSize(textBytes, i18n.language), max: formatBytes(textLimit, i18n.language) })}
              </span>
            </div>

            <div
              class={['pointer-events-none absolute inset-0 overflow-hidden rounded-t-card transition-opacity duration-500', phase === 'encrypting' ? 'opacity-100' : 'opacity-0']}
              aria-hidden="true"
            >
              <p
                class="absolute inset-0 px-6 pt-6 font-mono text-[13px] leading-6 break-all text-aegis-200/35 [mask-image:radial-gradient(ellipse_62%_58%_at_50%_50%,transparent_35%,black_100%)]"
              >
                {cipher}
              </p>
              <div class="absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-400/12 to-transparent"></div>
              <div class="absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"></div>
              <div class="absolute inset-0 flex flex-col items-center justify-center px-4 pb-4">
                <Pipeline {nodes} current={shown.step} progress={shown.progress} active={phase === 'encrypting'} />
              </div>
            </div>

            <div
              class="pointer-events-none absolute inset-0 rounded-t-card opacity-0 shadow-[inset_0_0_0_2px_oklch(0.77_0.115_249/0.7)] transition-opacity duration-500 group-focus-within:opacity-100"
              aria-hidden="true"
            ></div>
            <div
              class={[
                'pointer-events-none absolute inset-2 grid place-items-center rounded-pane border border-dashed border-aegis-300/60 bg-void-950/80 transition-opacity duration-300',
                dragging ? 'opacity-100' : 'opacity-0',
              ]}
              aria-hidden="true"
            >
              <p class="flex items-center gap-3 px-4 text-center text-aegis-200">
                <span class="grid size-9 shrink-0 place-items-center rounded-control bg-aegis-500/15 ring-1 ring-aegis-400/35">
                  <Icon name="upload" class="size-[18px]" />
                </span>
                {t('compose.drop')}
              </p>
            </div>
          </div>

          <div class="relative border-t border-white/8">
            <div class="relative flex flex-col gap-2 p-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
              <div class={['grid grid-cols-2 gap-1.5 transition-opacity duration-500 sm:flex sm:items-center', busy ? 'pointer-events-none opacity-40' : 'opacity-100']} inert={busy}>
                <div class="sm:relative" data-menu>
                  <button
                    id="{uid}-ttl-trigger"
                    type="button"
                    aria-haspopup="dialog"
                    aria-expanded={menu === 'ttl'}
                    aria-controls="{uid}-ttl"
                    aria-label={t('compose.ttl.label', { ttl: ttlLabel(ttlSeconds) })}
                    onclick={() => toggle('ttl')}
                    class={[chipBase, menu === 'ttl' ? chipOpen : problemOf.ttl ? chipProblem : chipIdle]}
                  >
                    <Icon name="clock" class="size-4 shrink-0" />
                    {ttlLabel(ttlSeconds)}
                    <Icon name="chevronDown" stroke={2} class={['size-3.5 shrink-0 opacity-60 transition-transform duration-300', menu === 'ttl' ? 'rotate-180' : 'rotate-0']} />
                  </button>
                  <Popover
                    id="{uid}-ttl"
                    label={t('compose.ttl.title')}
                    open={menu === 'ttl'}
                    onclose={() => closeMenu('ttl')}
                    class="inset-x-0 top-full mt-2 p-1 sm:inset-x-auto sm:left-0 sm:w-56"
                  >
                    <p class="px-2.5 pt-1.5 pb-1 text-[11px] text-ink-mute">{t('compose.ttl.title')}</p>
                    {#each choices as seconds (seconds)}
                      <button
                        type="button"
                        aria-pressed={ttlSeconds === seconds}
                        onclick={() => {
                          ttlSeconds = seconds;
                          closeMenu('ttl');
                        }}
                        class={[
                          'flex h-11 w-full items-center justify-between rounded-[7px] px-2.5 text-[13px] hover:bg-white/6 md:h-8',
                          ttlSeconds === seconds ? 'text-ink' : 'text-ink-soft',
                        ]}
                      >
                        {ttlLabel(seconds)}
                        <Icon name="check" stroke={2.2} class={['size-3.5 text-aegis-300 transition-opacity duration-300', ttlSeconds === seconds ? 'opacity-100' : 'opacity-0']} />
                      </button>
                    {/each}
                    <p class="px-2.5 pt-1.5 pb-1.5 text-[11px] leading-relaxed text-ink-mute">{t('compose.ttl.note')}</p>
                  </Popover>
                </div>

                <div class="sm:relative" data-menu>
                  <button
                    id="{uid}-opens-trigger"
                    type="button"
                    aria-haspopup="dialog"
                    aria-expanded={menu === 'opens'}
                    aria-controls="{uid}-opens"
                    onclick={() => toggle('opens')}
                    class={[chipBase, menu === 'opens' ? chipOpen : problemOf.opens ? chipProblem : chipIdle]}
                  >
                    <Icon name="eye" class="size-4 shrink-0" />
                    {opensLabel(opensMax)}
                    <Icon name="chevronDown" stroke={2} class={['size-3.5 shrink-0 opacity-60 transition-transform duration-300', menu === 'opens' ? 'rotate-180' : 'rotate-0']} />
                  </button>
                  <Popover
                    id="{uid}-opens"
                    label={t('compose.opens.title')}
                    open={menu === 'opens'}
                    onclose={() => closeMenu('opens')}
                    class="inset-x-0 top-full mt-2 p-2 sm:inset-x-auto sm:left-0 sm:w-52"
                  >
                    <p class="px-1 pt-0.5 text-[11px] text-ink-mute">{t('compose.opens.title')}</p>
                    <div class="mt-1.5 flex items-center justify-between rounded-[8px] well p-0.5">
                      <button
                        type="button"
                        disabled={opensMax <= 1}
                        onclick={() => (opensMax = Math.max(1, opensMax - 1))}
                        aria-label={t('compose.opens.less')}
                        class="grid size-11 place-items-center rounded-[7px] text-ink-soft hover:bg-white/8 hover:text-ink disabled:opacity-40 md:size-8"
                      >
                        <Icon name="minus" class="size-3.5" stroke={2} />
                      </button>
                      <output class="text-[15px] font-medium text-ink tabular-nums" aria-live="polite">{opensMax}</output>
                      <button
                        type="button"
                        disabled={opensMax >= maxOpens}
                        onclick={() => (opensMax = Math.min(maxOpens, opensMax + 1))}
                        aria-label={t('compose.opens.more')}
                        class="grid size-11 place-items-center rounded-[7px] text-ink-soft hover:bg-white/8 hover:text-ink disabled:opacity-40 md:size-8"
                      >
                        <Icon name="plus" class="size-3.5" stroke={2} />
                      </button>
                    </div>
                    <p class="mt-1.5 px-1 pb-0.5 text-[11px] leading-relaxed text-ink-mute">{t('compose.opens.note', { max: maxOpens })}</p>
                  </Popover>
                </div>

                <div class="sm:relative" data-menu>
                  <button
                    id="{uid}-security-trigger"
                    type="button"
                    aria-haspopup="dialog"
                    aria-expanded={menu === 'security'}
                    aria-controls="{uid}-security"
                    onclick={() => toggle('security')}
                    class={[chipBase, menu === 'security' ? chipOpen : problemOf.passphrase ? chipProblem : protect ? chipAccent : chipIdle]}
                  >
                    <Icon name="key" class="size-4 shrink-0" />
                    {t('compose.passphrase.chip')}
                    <Icon name="chevronDown" stroke={2} class={['size-3.5 shrink-0 opacity-60 transition-transform duration-300', menu === 'security' ? 'rotate-180' : 'rotate-0']} />
                  </button>
                  <Popover
                    id="{uid}-security"
                    label={t('compose.security')}
                    open={menu === 'security'}
                    onclose={() => closeMenu('security')}
                    class="inset-x-0 top-full mt-2 p-1 sm:inset-x-auto sm:left-0 sm:w-72"
                  >
                    <button type="button" role="switch" aria-checked={protect} onclick={toggleProtect} class="flex w-full items-start gap-3 rounded-[7px] p-2 text-left hover:bg-white/4">
                      <span class="mt-px"><Switch on={protect} /></span>
                      <span>
                        <span class="block text-[13px] text-ink">{t('compose.passphrase.switch')}</span>
                        <span class="mt-0.5 block text-[11px] leading-relaxed text-ink-mute">{t('compose.passphrase.switch.note')}</span>
                      </span>
                    </button>
                    <div class={['flex flex-col gap-1.5 px-2 pt-0.5 pb-2 transition-opacity duration-500', protect ? 'opacity-100' : 'opacity-40']}>
                      <label for="{uid}-passphrase" class="sr-only">{t('create.passphrase')}</label>
                      <input
                        id="{uid}-passphrase"
                        bind:value={passphrase}
                        type="password"
                        disabled={!protect}
                        autocomplete="off"
                        autocapitalize="off"
                        spellcheck="false"
                        data-1p-ignore=""
                        data-lpignore="true"
                        data-bwignore="true"
                        data-form-type="other"
                        placeholder={t('compose.passphrase.placeholder', { words: WEAK_BELOW_WORDS })}
                        aria-invalid={problemOf.passphrase !== undefined}
                        aria-describedby="{uid}-passphrase-hint {uid}-passphrase-problem"
                        class={[
                          'well h-11 w-full rounded-[7px] px-3 text-[13px] text-ink placeholder:text-ink-mute md:h-8',
                          problemOf.passphrase && 'shadow-[inset_0_0_0_1px_oklch(0.8_0.08_350/0.7)]',
                        ]}
                      />
                      <p id="{uid}-passphrase-hint" class="text-[11px] leading-relaxed text-ink-mute">{t('create.passphrase.hint', { min: MIN_PASSPHRASE_CHARS })}</p>
                      <p id="{uid}-passphrase-problem" class={['min-h-[1lh] text-[11px] leading-relaxed', problemOf.passphrase ? 'text-rose-300' : 'text-nebula-300']}>{passphraseMessage}</p>
                    </div>
                    <div class="mx-2 h-px bg-white/8"></div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={remember}
                      onclick={() => (remember = !remember)}
                      class="flex w-full items-start gap-3 rounded-[7px] p-2 text-left hover:bg-white/4"
                    >
                      <span class="mt-px"><Switch on={remember} /></span>
                      <span>
                        <span class="block text-[13px] text-ink">{t('compose.remember.switch')}</span>
                        <span class="mt-0.5 block text-[11px] leading-relaxed text-ink-mute">{t('compose.remember.note')}</span>
                      </span>
                    </button>
                  </Popover>
                </div>

                <div class="relative">
                  <input
                    id="{uid}-file"
                    type="file"
                    class="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0"
                    onchange={(event) => pickFile(event.currentTarget.files?.[0])}
                    aria-invalid={problemOf.file !== undefined}
                    aria-describedby="{uid}-notice"
                  />
                  <label
                    for="{uid}-file"
                    class={[
                      chipBase,
                      'peer-hover:bg-white/8 peer-hover:text-ink peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-aegis-300',
                      problemOf.file ? chipProblem : file ? 'bg-nebula-400/12 text-nebula-300 ring-nebula-300/35' : chipIdle,
                    ]}
                  >
                    <Icon name="paperclip" class="size-4 shrink-0" />
                    {t('compose.attach')}<span class="sr-only">{t('compose.attach.limit', { size: formatBytes(fileLimit, i18n.language) })}</span>
                  </label>
                </div>
              </div>

              <div class="stack shrink-0">
                <button
                  id="{uid}-submit"
                  type="submit"
                  disabled={!limits || busy}
                  class={[
                    'btn-primary hover-lift flex h-11 w-full items-center justify-center gap-2 rounded-[9px] px-5 text-sm font-medium whitespace-nowrap transition-[opacity,translate,scale] duration-500 md:h-9 md:px-4',
                    busy ? 'pointer-events-none opacity-0' : 'opacity-100',
                  ]}
                >
                  <span class="sheen" aria-hidden="true"></span>
                  <Icon name="lock" stroke={2} class="size-4 shrink-0" />
                  <span class="hidden lg:inline">{t('compose.submit')}</span>
                  <span class="lg:hidden">{t('create.submit')}</span>
                </button>
                <div
                  class={[
                    'relative flex h-11 items-center justify-center gap-2.5 overflow-hidden rounded-[9px] bg-aegis-950/70 px-5 text-sm font-medium whitespace-nowrap text-ink ring-1 ring-aegis-400/45 transition-opacity duration-500 md:h-9 md:px-4',
                    busy ? 'opacity-100' : 'pointer-events-none opacity-0',
                  ]}
                  aria-hidden={!busy}
                >
                  <progress class="progress-fill absolute inset-0 h-full w-full" max="1" value={shown.progress} aria-hidden="true"></progress>
                  <span class="absolute inset-y-0 left-0 w-1/3 animate-beam bg-linear-to-r from-transparent via-white/15 to-transparent" aria-hidden="true"></span>
                  <svg class="relative size-4 shrink-0 animate-spin" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-opacity="0.25" stroke-width="2.2"></circle>
                    <path d="M20.5 12A8.5 8.5 0 0 0 12 3.5" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"></path>
                  </svg>
                  <span class="relative">{t('compose.encrypting')}</span>
                  <span class="relative inline-block w-[4ch] font-mono text-[13px] whitespace-nowrap text-white/75 tabular-nums">{Math.round(shown.progress * 100)} %</span>
                </div>
              </div>
            </div>
          </div>
        </form>

        {#if sealed}
          <CreatedPanel created={sealed.created} opensMax={sealed.opensMax} passphrase={sealed.passphrase} onanother={another} />
        {/if}
      </div>
    </div>
  </div>

  <div class="relative mt-4 text-xs">
    <p class={['fade-swap flex items-center justify-center gap-2 text-ink-mute', notice === '' && remembered === 'off' ? 'opacity-100' : 'pointer-events-none opacity-0']}>
      <Icon name="shieldCheck" stroke={1.7} class="size-4 shrink-0 text-aegis-300" />
      {t('compose.note')}
    </p>
    <div class={['fade-swap absolute inset-x-0 top-1/2 flex -translate-y-1/2 items-center justify-center gap-3 text-center', notice === '' ? 'pointer-events-none opacity-0' : 'opacity-100']}>
      <p id="{uid}-notice" role="alert" class="text-rose-300">{notice}</p>
      <button
        type="button"
        onclick={loadLimits}
        inert={!loadFailed}
        class={['btn-ghost h-11 rounded-[9px] px-3 text-xs md:h-8', loadFailed ? 'opacity-100' : 'pointer-events-none opacity-0']}
      >
        {t('action.retry')}
      </button>
    </div>
    <div class={['fade-swap absolute inset-x-0 top-1/2 flex -translate-y-1/2 flex-wrap items-center justify-center gap-x-3 gap-y-1 text-center', remembered === 'remembered' || remembered === 'failed' ? 'opacity-100' : 'pointer-events-none opacity-0']} inert={remembered === 'off' || remembered === 'saving'}>
      <p role="status" class={['flex items-center gap-2', remembered === 'failed' ? 'text-rose-300' : 'text-ink-mute']}>
        <Icon name={remembered === 'failed' ? 'alert' : 'check'} class="size-4 shrink-0 text-aegis-300" />
        {remembered === 'remembered' ? t('created.remembered') : remembered === 'failed' ? t('created.not_remembered') : ''}
      </p>
      <a href="/sent" class="inline-flex items-center gap-1.5 text-aegis-300 hover:text-aegis-200">
        {t('created.status_link')}
        <Icon name="arrowRight" class="size-3.5" />
      </a>
    </div>
  </div>
  <p class="sr-only" role="status" aria-live="polite">{stepTitle}</p>
</section>
