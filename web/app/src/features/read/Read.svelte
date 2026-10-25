<script lang="ts">
  import { tick } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import { prefersReducedMotion } from 'svelte/motion';

  import ReportForm from '../../shared/report/ReportForm.svelte';
  import Chip from '../../shared/ui/Chip.svelte';
  import Icon from '../../shared/ui/Icon.svelte';
  import Pipeline from '../../shared/pipeline/Pipeline.svelte';
  import Popover from '../../shared/ui/Popover.svelte';
  import { createApi, type Metadata } from '../../shared/api/api.ts';
  import { fileExtension, isKnownSafeFile, saveFile, splitFileName } from '../../shared/files/file-name.ts';
  import { formatBytes, formatWait } from '../../shared/language/format.ts';
  import { keepFocus } from '../../shared/ui/focus.ts';
  import { getTranslator } from '../../shared/language/translator.svelte.ts';
  import { type MessageKey } from '../../shared/language/czech.ts';
  import type { IconName } from '../../shared/ui/icons.ts';
  import { scryptInWorker } from '../../shared/kdf/kdf.ts';
  import { pace } from '../../shared/pipeline/pacing.ts';
  import type { PipelineNode } from '../../shared/pipeline/pipeline.ts';
  import { createReader, type BurnOutcome, type BusyStep, type ReadView, type RevealedFile } from './reader.ts';

  type Notice =
    | 'loading'
    | 'opened'
    | 'expired'
    | 'cancelled'
    | 'deleted'
    | 'burned'
    | 'locked'
    | 'unknown'
    | 'invalid'
    | 'newer'
    | 'retired'
    | 'tampered'
    | 'failed'
    | 'busy'
    | 'cleared';

  interface ChipData {
    readonly icon: IconName;
    readonly text: string;
    readonly accent?: boolean;
  }

  const MIN_MS = { passphrase: 3800, plain: 2600, reduced: 1000 } as const;
  const TICK_MS = 60;
  const REVEAL_MS = 900;
  const REVEAL_TICK_MS = 40;
  const SCRAMBLED_CHARS = 2000;
  const FLASH_MS = 2400;
  const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
  const SHOWN = 'translate-y-0 opacity-100';
  const HIDDEN = 'pointer-events-none translate-y-3 opacity-0';
  const STEPS = [
    { id: 'derive', busy: 'deriving', icon: 'key' },
    { id: 'claim', busy: 'claiming', icon: 'envelopeOpen' },
    { id: 'download', busy: 'downloading', icon: 'download' },
    { id: 'decrypt', busy: 'decrypting', icon: 'unlock' },
  ] as const;
  const NOTICE_ICONS: Record<Notice, IconName> = {
    loading: 'envelope',
    opened: 'envelopeOpen',
    expired: 'clock',
    cancelled: 'cancel',
    deleted: 'trash',
    burned: 'trash',
    locked: 'lock',
    unknown: 'search',
    invalid: 'broken',
    newer: 'update',
    retired: 'key',
    tampered: 'shield',
    failed: 'alert',
    busy: 'clock',
    cleared: 'eyeOff',
  };
  const ALARMING: readonly Notice[] = ['locked', 'invalid', 'tampered', 'failed'];
  const FAILED_BURNS: readonly BurnOutcome['outcome'][] = ['failed', 'retry_later', 'unknown'];
  const TRUST = [
    { key: 'open.trust.key', icon: 'key', reveal: 'reveal-up' },
    { key: 'open.trust.click', icon: 'eye', reveal: 'reveal-up reveal-step-1' },
    { key: 'open.trust.gone', icon: 'trash', reveal: 'reveal-up reveal-step-2' },
  ] as const;

  const translator = getTranslator();
  const { translate } = translator;
  const uid = $props.id();

  let view = $state.raw<ReadView>({ view: 'loading' });
  let shown = $state.raw<ReadView>({ view: 'loading' });
  let passphrase = $state('');
  let showPassphrase = $state(false);
  let emptyPassphrase = $state(false);
  let typedAgain = $state(false);
  let passphraseRequired = $state(false);
  let metadata = $state.raw<Metadata>();
  let confirming = $state(false);
  let askingDelete = $state(false);
  let reporting = $state(false);
  let reportSent = $state(false);
  let reportRound = $state(0);
  let restDeleted = $state(false);
  let deleting = $state(false);
  let burned = $state.raw<{ readonly outcome: BurnOutcome; readonly afterReveal: boolean }>();
  let reached = $state(0);
  let fraction = $state<number | undefined>();
  let elapsed = $state(0);
  let opening = $state(false);
  let revealText = $state<string>();
  let copied = $state(false);
  let downloaded = $state(false);
  let noise = $state('');
  let pending: ReadView | undefined;
  let revealing = false;
  let markShown: (() => void) | undefined;
  let startedAt = 0;
  let timer: ReturnType<typeof setInterval> | undefined;
  let revealTimer: ReturnType<typeof setInterval> | undefined;
  let copiedTimer: ReturnType<typeof setTimeout> | undefined;
  let downloadedTimer: ReturnType<typeof setTimeout> | undefined;

  const steps = $derived(passphraseRequired ? STEPS : STEPS.slice(1));
  const nodes: readonly PipelineNode[] = $derived(
    steps.map(({ id, icon }) => ({
      id,
      label: translate(`open.step.${id}.label`),
      icon,
      title: translate(`open.step.${id}.title`),
      detail: translate(`open.step.${id}.detail`),
    })),
  );
  const minMs = $derived(
    prefersReducedMotion.current ? MIN_MS.reduced : passphraseRequired ? MIN_MS.passphrase : MIN_MS.plain,
  );
  const paced = $derived(pace({ elapsedMs: elapsed, minMs, steps: nodes.length, reached, fraction }));
  const ready = $derived(shown.view === 'ready' ? shown : undefined);
  const revealed = $derived(shown.view === 'revealed' ? shown : undefined);
  const locked = $derived(ready?.rejected?.reason === 'wrong' && ready.rejected.locked);
  const phase = $derived(
    opening ? 'opening' : revealed ? 'revealed' : (ready && !locked) || shown.view === 'loading' ? 'ready' : 'notice',
  );
  const notice = $derived(noticeOf(shown));
  const burnNote = $derived(burned ? burnText(burned.outcome, burned.afterReveal) : '');
  const burnFailed = $derived(burned !== undefined && FAILED_BURNS.includes(burned.outcome.outcome));
  const status = $derived.by(() => {
    if (shown.view === 'loading') {
      return translate('read.loading');
    }
    if (opening && view.view === 'busy') {
      return translate(`busy.${view.step}`);
    }
    if (reporting && reportSent) {
      return translate('open.report.sent');
    }
    return burnNote;
  });
  const passError = $derived.by(() => {
    if (emptyPassphrase) {
      return translate('open.passphrase.empty');
    }
    const rejected = typedAgain ? undefined : ready?.rejected;
    if (rejected?.reason === 'malformed') {
      return translate('read.rejected.malformed');
    }
    if (rejected?.reason === 'wrong_or_changed') {
      return translate('read.rejected.wrong_or_changed');
    }
    if (rejected && rejected.retryAfter > 0) {
      return translate('read.rejected.wait', { time: formatWait(rejected.retryAfter, translator.language) });
    }
    return rejected ? translate('read.rejected.wrong', { count: rejected.attemptsLeft }) : '';
  });
  const readyNote = $derived.by(() => {
    if (ready?.deviceFailed) {
      return { alert: true, text: translate('read.device') };
    }
    if (ready?.leaseEnded) {
      return { alert: true, text: translate('read.lease_ended') };
    }
    return { alert: false, text: ready?.resumable ? translate('read.resumable') : '' };
  });
  const passHint = $derived(readyNote.text || translate('open.passphrase.hint'));
  const passAlert = $derived(passError || (readyNote.alert ? readyNote.text : ''));
  const readyChips = $derived.by((): ChipData[] => {
    if (!metadata) {
      return [];
    }
    const chips: ChipData[] = [];
    if (metadata.expiresAt) {
      chips.push({ icon: 'clock', text: translate('open.chip.expires', { time: shortTime(metadata.expiresAt) }) });
    }
    chips.push({ icon: 'eye', text: opensLeft(metadata.opensRemaining) });
    if (metadata.passphraseRequired) {
      chips.push({ icon: 'key', text: translate('compose.passphrase.chip'), accent: true });
    }
    return chips;
  });
  const afterChips = $derived.by((): ChipData[] => {
    if (revealed) {
      return [
        { icon: 'check', text: translate('open.chip.now') },
        { icon: 'eye', text: revealed.burnable && !restDeleted ? translate('open.chip.more') : opensLeft(0) },
      ];
    }
    if (notice === 'burned') {
      return [{ icon: 'check', text: translate('open.chip.burned') }];
    }
    if (shown.view === 'ended' && shown.firstOpenedAt) {
      return [{ icon: 'eye', text: translate('open.chip.first_opened', { time: shortTime(shown.firstOpenedAt) }) }];
    }
    if (shown.view === 'ended' && shown.endedAt) {
      return [{ icon: 'clock', text: translate('open.chip.ended', { time: shortTime(shown.endedAt) }) }];
    }
    return [];
  });
  const actions = $derived(
    phase === 'opening'
      ? 'dim'
      : (phase === 'ready' || phase === 'revealed' || locked) && shown.view !== 'loading' && !askingDelete
        ? 'on'
        : 'off',
  );
  const deleteActions = $derived([
    { id: 'delete', label: 'read.delete', shown: ready !== undefined, enabled: ready !== undefined && !deleting },
    {
      id: 'delete-rest',
      label: 'read.delete.rest',
      shown: revealed !== undefined && (revealed.burnable || restDeleted),
      enabled: (revealed?.burnable ?? false) && !deleting,
    },
  ] as const);
  const reportable = $derived(ready !== undefined || (revealed?.burnable ?? false));

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
      shown: displayed,
      scrypt: (pass, saltPw, onProgress) => scryptInWorker(pass, saltPw, { onProgress }),
      now: () => Date.now(),
    },
    onView,
  );

  reader.load();

  function displayed(): Promise<void> {
    return new Promise((resolve) => {
      if (shown.view === 'revealed') {
        resolve();
      } else {
        markShown = resolve;
      }
    });
  }

  function settleShown() {
    markShown?.();
    markShown = undefined;
  }

  function stepIndex(step: BusyStep): number {
    return Math.max(
      0,
      steps.findIndex((item) => item.busy === step),
    );
  }

  function onView(next: ReadView) {
    const wasOpening = opening;
    view = next;
    if (next.view === 'ready') {
      passphraseRequired = next.metadata.passphraseRequired;
      metadata = next.metadata;
      typedAgain = false;
      emptyPassphrase = false;
    }
    if (next.view === 'busy') {
      if (!opening) {
        startTimeline();
      }
      reached = stepIndex(next.step);
      fraction = next.fraction;
      return;
    }
    if (next.view === 'revealed' && opening) {
      pending = next;
      reached = nodes.length;
      fraction = undefined;
      return;
    }
    pending = undefined;
    stopTimeline();
    show(next);
    if (wasOpening && next.view === 'ready' && passphraseRequired) {
      tick().then(() => document.getElementById(`${uid}-passphrase`)?.focus());
    }
  }

  function show(next: ReadView) {
    const wasRevealed = shown.view === 'revealed';
    shown = next;
    if (next.view === 'revealed') {
      settleShown();
      if (!wasRevealed) {
        reportSent = false;
        reportRound++;
        startReveal(next.text);
      }
    } else {
      revealText = undefined;
    }
  }

  function startTimeline() {
    opening = true;
    startedAt = performance.now();
    elapsed = 0;
    reached = 0;
    fraction = undefined;
    noise = randomText(260);
    const active = document.activeElement;
    if (active && [`${uid}-ready`, `${uid}-bar`].some((id) => document.getElementById(id)?.contains(active))) {
      document.getElementById('read-title')?.focus();
    }
    clearInterval(timer);
    timer = setInterval(() => {
      elapsed = performance.now() - startedAt;
      noise = randomText(260 + Math.floor(paced.progress * 900));
      if (paced.finished && pending) {
        const next = pending;
        pending = undefined;
        stopTimeline();
        show(next);
      }
    }, TICK_MS);
  }

  function stopTimeline() {
    clearInterval(timer);
    timer = undefined;
    opening = false;
  }

  async function startReveal(text: string | undefined) {
    clearInterval(revealTimer);
    revealText = text;
    if (text !== undefined && !prefersReducedMotion.current) {
      const started = performance.now();
      const chars = Array.from(text.slice(0, SCRAMBLED_CHARS));
      const rest = text.slice(chars.join('').length);
      revealText = scramble(chars, 0) + rest;
      revealTimer = setInterval(() => {
        const progress = Math.min(1, (performance.now() - started) / REVEAL_MS);
        revealText = progress >= 1 ? text : scramble(chars, progress) + rest;
        if (progress >= 1) {
          clearInterval(revealTimer);
        }
      }, REVEAL_TICK_MS);
    }
    await tick();
    document.getElementById(`${uid}-revealed`)?.focus();
  }

  function randomChar(): string {
    return ALPHABET[Math.floor(Math.random() * ALPHABET.length)] ?? '';
  }

  function randomText(length: number): string {
    return Array.from({ length }, randomChar).join('');
  }

  function scramble(chars: readonly string[], progress: number): string {
    const cut = Math.floor(chars.length * Math.min(1, progress * 1.15));
    return chars
      .map((char, index) => (index < cut || char === ' ' || char === '\n' || char === '\t' ? char : randomChar()))
      .join('');
  }

  function shortTime(iso: string): string {
    return new Intl.DateTimeFormat(translator.language, {
      day: 'numeric',
      month: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    }).format(new Date(iso));
  }

  function opensLeft(count: number): string {
    const rule = new Intl.PluralRules(translator.language).select(count);
    const key: MessageKey =
      rule === 'one' ? 'open.chip.opens.one' : rule === 'few' ? 'open.chip.opens.few' : 'open.chip.opens.other';
    return translate(key, { count });
  }

  function noticeOf(current: ReadView): Notice {
    switch (current.view) {
      case 'ended':
        return current.state === 'deleted' && burned?.outcome.outcome === 'deleted' && !burned.afterReveal
          ? 'burned'
          : current.state;
      case 'ready':
        return current.rejected?.reason === 'wrong' && current.rejected.locked ? 'locked' : 'loading';
      case 'retry_later':
        return 'busy';
      case 'invalid':
      case 'newer':
      case 'retired':
      case 'unknown':
      case 'tampered':
      case 'failed':
      case 'cleared':
        return current.view;
      default:
        return 'loading';
    }
  }

  function noticeTitle(kind: Notice): string {
    return kind === 'loading' ? translate('read.loading') : translate(`open.notice.${kind}`);
  }

  function noticeText(kind: Notice): string {
    switch (kind) {
      case 'unknown':
        return translate('read.unknown');
      case 'cleared':
        return translate('read.cleared');
      case 'busy':
        return translate('open.notice.busy.text', {
          time: formatWait(shown.view === 'retry_later' ? shown.retryAfter : undefined, translator.language),
        });
      case 'loading':
        return translate('open.loading.text');
      default:
        return translate(`open.notice.${kind}.text`);
    }
  }

  function reveal(event: SubmitEvent) {
    event.preventDefault();
    if (revealing || phase !== 'ready' || shown.view !== 'ready' || deleting) {
      return;
    }
    if (passphraseRequired && passphrase === '') {
      emptyPassphrase = true;
      document.getElementById(`${uid}-passphrase`)?.focus();
      return;
    }
    emptyPassphrase = false;
    showPassphrase = false;
    reporting = false;
    askingDelete = false;
    burned = undefined;
    const entered = passphrase;
    passphrase = '';
    revealing = true;
    reader.reveal(entered).finally(() => (revealing = false));
  }

  function typed() {
    emptyPassphrase = false;
    typedAgain = true;
  }

  function burnText(outcome: BurnOutcome, afterReveal: boolean): string {
    switch (outcome.outcome) {
      case 'deleted':
        return afterReveal ? translate('read.deleted_rest') : '';
      case 'ended':
        return afterReveal ? translate(`read.ended.${outcome.state}`) : '';
      case 'retry_later':
        return translate('read.retry_later', { time: formatWait(outcome.retryAfter, translator.language) });
      case 'unknown':
        return afterReveal ? translate('read.delete.failed') : '';
      case 'failed':
        return translate('read.delete.failed');
      case 'ignored':
        return '';
    }
  }

  async function askDelete() {
    reporting = false;
    burned = undefined;
    askingDelete = true;
    await tick();
    document.getElementById(`${uid}-keep`)?.focus();
  }

  async function cancelDelete() {
    askingDelete = false;
    await tick();
    document.getElementById(revealed ? `${uid}-delete-rest` : `${uid}-delete`)?.focus();
  }

  function onconfirmkey(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      cancelDelete();
    }
  }

  async function burn() {
    askingDelete = false;
    burned = undefined;
    deleting = true;
    const afterReveal = shown.view === 'revealed';
    const outcome = await reader.burn();
    deleting = false;
    restDeleted ||= afterReveal && (outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    burned = { outcome, afterReveal };
    await tick();
    keepFocus(document.getElementById(afterReveal ? `${uid}-revealed` : 'read-title'));
  }

  function toggleReport() {
    reporting = !reporting;
  }

  async function closeReport() {
    reporting = false;
    await tick();
    document.getElementById(`${uid}-report-button`)?.focus();
  }

  async function reportAccepted() {
    reportSent = true;
    await tick();
    if (reporting) {
      document.getElementById(`${uid}-report`)?.focus();
    }
  }

  function leave() {
    burned = undefined;
    confirming = false;
    askingDelete = false;
    reporting = false;
    showPassphrase = false;
    passphrase = '';
    pending = undefined;
    revealText = undefined;
    clearInterval(revealTimer);
    stopTimeline();
    reader.clear();
    settleShown();
  }

  function returned(event: PageTransitionEvent) {
    if (event.persisted) {
      location.reload();
    }
  }

  async function askDownload(event: MouseEvent, file: RevealedFile) {
    if (event.detail > 1) {
      return;
    }
    if (isKnownSafeFile(file.name)) {
      save(file);
      return;
    }
    confirming = true;
    await tick();
    document.getElementById(`${uid}-keep-file`)?.focus();
  }

  function confirmDownload(event: MouseEvent, file: RevealedFile) {
    if (event.detail > 1 || !confirming) {
      return;
    }
    save(file);
    focusDownload();
  }

  function cancelDownload(event: MouseEvent) {
    if (event.detail > 1) {
      return;
    }
    confirming = false;
    focusDownload();
  }

  async function focusDownload() {
    await tick();
    document.getElementById(`${uid}-download`)?.focus();
  }

  function save(file: RevealedFile) {
    confirming = false;
    saveFile(file.bytes, file.name);
    downloaded = true;
    clearTimeout(downloadedTimer);
    downloadedTimer = setTimeout(() => (downloaded = false), FLASH_MS);
  }

  async function copyText() {
    if (!revealed?.text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(revealed.text);
      copied = true;
      clearTimeout(copiedTimer);
      copiedTimer = setTimeout(() => (copied = false), FLASH_MS);
    } catch {
      copied = false;
    }
  }

  function onwindowclick(event: MouseEvent) {
    if (reporting && !(event.target instanceof Element && event.target.closest('[data-menu]'))) {
      reporting = false;
    }
  }

  const cleanup: Attachment = () => () => {
    clearInterval(timer);
    clearInterval(revealTimer);
    clearTimeout(copiedTimer);
    clearTimeout(downloadedTimer);
  };
</script>

<svelte:window onpagehide={leave} onpageshow={returned} onclick={onwindowclick} />

{#snippet chips(list: readonly ChipData[], dim = false)}
  <ul
    class={[
      'flex flex-wrap items-center gap-1.5 transition-opacity duration-500',
      dim ? 'pointer-events-none opacity-40' : 'opacity-100',
    ]}
  >
    {#each list as data (data.text)}
      <Chip icon={data.icon} accent={data.accent ?? false}>{data.text}</Chip>
    {/each}
  </ul>
{/snippet}

{#snippet swapIcons(first: IconName, second: IconName, swapped: boolean, tone = 'text-aegis-200')}
  <span class="stack place-items-center">
    <Icon
      name={first}
      class={[
        'size-4 transition-[opacity,scale] duration-300 ease-silk',
        swapped ? 'scale-50 opacity-0' : 'scale-100 opacity-100',
      ]}
    />
    <Icon
      name={second}
      stroke={2.2}
      class={[
        'size-4 transition-[opacity,scale] duration-300 ease-silk',
        tone,
        swapped ? 'scale-100 opacity-100' : 'scale-50 opacity-0',
      ]}
    />
  </span>
{/snippet}

<section id="otevrit" class="px-4 pt-20 pb-20 sm:px-6 md:pt-28 md:pb-28" aria-labelledby="read-title" {@attach cleanup}>
  <p class="sr-only" role="status" aria-live="polite">{status}</p>
  <div class="mx-auto max-w-[860px] text-center">
    <p class="animate-rise text-sm font-medium text-aegis-300" aria-hidden="true">{translate('read.title')}</p>
    <h1
      class="mt-3 animate-rise font-display text-[36px] leading-[1.12] font-light tracking-[-0.025em] text-balance [animation-delay:80ms] sm:text-[50px] lg:text-[62px]"
    >
      {translate('open.title')}
    </h1>
    <p
      class="mx-auto mt-6 max-w-[620px] animate-rise text-base leading-[1.75] text-balance text-ink-soft [animation-delay:160ms] sm:text-[17px]"
    >
      {translate('open.intro')}
    </p>
  </div>

  <div class="relative z-20 mx-auto mt-14 max-w-[860px] animate-rise [animation-delay:240ms]">
    <div class="glow-under" aria-hidden="true"></div>
    <div class="glass rounded-card">
      <div class="ring-light" aria-hidden="true"><div class="ring-light-spin animate-spin-slow"></div></div>
      <h2 id="read-title" tabindex="-1" class="sr-only">{translate('read.title')}</h2>

      <div class="relative stack min-h-[420px] md:min-h-[440px]">
        <div
          id="{uid}-ready"
          class={[
            'flex flex-col items-center justify-center gap-7 px-5 py-12 text-center fade-swap sm:px-10 md:py-14',
            phase === 'ready' ? SHOWN : HIDDEN,
          ]}
          inert={phase !== 'ready'}
        >
          <span class="relative grid size-20 place-items-center" aria-hidden="true">
            <span
              class="absolute -inset-5 rounded-full bg-[radial-gradient(closest-side,oklch(0.56_0.19_258/0.35),transparent)]"
            ></span>
            <span class="absolute -inset-3 rounded-[28px] ring-1 ring-aegis-300/15"></span>
            <span class="absolute -inset-1.5 animate-pulse-core rounded-[25px] ring-1 ring-aegis-300/30"></span>
            <span
              class="relative grid size-20 place-items-center rounded-[22px] bg-aegis-500/15 text-aegis-200 shadow-[0_0_44px_-8px_oklch(0.56_0.19_258/0.85)] ring-1 ring-aegis-400/40"
            >
              <Icon name="envelope" stroke={1.4} class="size-9" />
            </span>
            <span
              class="absolute -right-2 -bottom-2 grid size-8 place-items-center rounded-full bg-void-900 text-aegis-200 shadow-[0_0_16px_-4px_oklch(0.56_0.19_258/0.9)] ring-1 ring-aegis-400/50"
            >
              <Icon name="keyhole" stroke={2} class="size-4" />
            </span>
          </span>
          <div>
            <h3 class="font-display text-2xl font-light tracking-[-0.02em] md:text-[28px]">
              {translate(shown.view === 'loading' ? 'read.loading' : 'open.ready.title')}
            </h3>
            <p class="mt-2 text-[15px] text-pretty text-ink-mute">
              {translate(shown.view === 'loading' ? 'open.loading.text' : 'open.ready.sub')}
            </p>
          </div>
          <form id="{uid}-form" class="contents" novalidate onsubmit={reveal}>
            {#if passphraseRequired}
              <div class="w-full max-w-[440px] text-left">
                <label for="{uid}-passphrase" class="sr-only">{translate('read.passphrase')}</label>
                <div class="relative rounded-pane">
                  <div
                    class="flex items-center gap-3 rounded-pane py-2 pr-2 pl-4 well has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-3 has-[input:focus-visible]:outline-aegis-300"
                  >
                    <Icon name="key" class="size-4 shrink-0 text-nebula-300" />
                    <input
                      id="{uid}-passphrase"
                      bind:value={passphrase}
                      type={showPassphrase ? 'text' : 'password'}
                      autocomplete="off"
                      autocapitalize="off"
                      autocorrect="off"
                      spellcheck="false"
                      data-1p-ignore=""
                      data-lpignore="true"
                      data-bwignore="true"
                      data-form-type="other"
                      placeholder={translate('open.passphrase.placeholder')}
                      aria-invalid={passAlert !== ''}
                      aria-describedby="{uid}-pass-hint {uid}-pass-alert"
                      oninput={typed}
                      class="h-11 min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-ink-mute md:h-10"
                    />
                    <button
                      type="button"
                      onclick={() => (showPassphrase = !showPassphrase)}
                      aria-label={translate(showPassphrase ? 'open.passphrase.hide' : 'open.passphrase.show')}
                      aria-pressed={showPassphrase}
                      class="grid size-11 shrink-0 place-items-center rounded-[8px] text-ink-mute hover:bg-white/6 hover:text-ink md:size-9"
                    >
                      <span class="stack place-items-center">
                        <Icon
                          name="eye"
                          class={[
                            'size-[18px] transition-[opacity,scale] duration-300 ease-silk',
                            showPassphrase ? 'scale-50 opacity-0' : 'scale-100 opacity-100',
                          ]}
                        />
                        <Icon
                          name="eyeOff"
                          class={[
                            'size-[18px] transition-[opacity,scale] duration-300 ease-silk',
                            showPassphrase ? 'scale-100 opacity-100' : 'scale-50 opacity-0',
                          ]}
                        />
                      </span>
                    </button>
                  </div>
                  <span
                    class={[
                      'pointer-events-none absolute inset-0 rounded-pane shadow-[0_0_0_1px_oklch(0.72_0.15_15/0.7),0_0_24px_-8px_oklch(0.65_0.2_15/0.7)] transition-opacity duration-500',
                      passAlert ? 'opacity-100' : 'opacity-0',
                    ]}
                    aria-hidden="true"
                  ></span>
                </div>
                <div class="mt-3 stack px-1 text-[13px]">
                  <p
                    id="{uid}-pass-hint"
                    class={['flex items-center gap-2 text-ink-mute fade-swap', passAlert ? 'opacity-0' : 'opacity-100']}
                  >
                    <Icon name="info" class="size-4 shrink-0" />
                    {passHint}
                  </p>
                  <p
                    id="{uid}-pass-alert"
                    role="alert"
                    class={['flex items-center gap-2 text-rose-300 fade-swap', passAlert ? 'opacity-100' : 'opacity-0']}
                  >
                    <Icon name="alert" class="size-4 shrink-0" />
                    {passAlert}
                  </p>
                </div>
              </div>
            {:else if readyNote.text}
              <p
                role={readyNote.alert ? 'alert' : undefined}
                class={[
                  'flex max-w-[440px] items-center gap-2 text-[13px]',
                  readyNote.alert ? 'text-rose-300' : 'text-ink-mute',
                ]}
              >
                <Icon name={readyNote.alert ? 'alert' : 'info'} class="size-4 shrink-0" />
                {readyNote.text}
              </p>
            {/if}
          </form>
        </div>

        <div class={['relative overflow-hidden fade-swap', phase === 'opening' ? SHOWN : HIDDEN]} aria-hidden="true">
          <p
            class="pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_62%_58%_at_50%_50%,transparent_35%,black_100%)] px-6 pt-6 font-mono text-[13px] leading-6 break-all text-aegis-200/30"
          >
            {noise}
          </p>
          <div
            class="pointer-events-none absolute inset-x-0 top-0 h-28 animate-scan bg-linear-to-b from-transparent via-aegis-400/12 to-transparent"
          ></div>
          <div
            class="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_62%_62%_at_50%_50%,oklch(0.13_0.04_270/0.92),transparent_88%)]"
          ></div>
          <div class="relative flex h-full flex-col items-center justify-center px-4 py-12">
            <Pipeline
              {nodes}
              size="narrow"
              current={paced.step}
              progress={paced.progress}
              active={phase === 'opening'}
            />
          </div>
        </div>

        <div
          class={[
            'absolute inset-0 flex flex-col justify-center gap-5 px-5 py-6 text-left fade-swap sm:px-8 sm:py-8',
            phase === 'revealed' ? SHOWN : HIDDEN,
          ]}
          inert={phase !== 'revealed'}
        >
          {#if revealed}
            <div class="flex items-center justify-between gap-3">
              <div class="flex min-w-0 items-center gap-3">
                <span
                  class="grid size-10 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 shadow-[0_0_28px_-6px_oklch(0.56_0.19_258/0.75)] ring-1 ring-aegis-400/35"
                  aria-hidden="true"
                >
                  <Icon name="unlock" stroke={1.7} class="size-5" />
                </span>
                <div class="min-w-0">
                  <h3 id="{uid}-revealed" tabindex="-1" class="text-[15px] text-ink outline-none">
                    {translate('revealed.title')}
                  </h3>
                  <p class="truncate text-[13px] text-ink-mute">{translate('open.revealed.sub')}</p>
                </div>
              </div>
              {#if revealed.text !== undefined}
                <button
                  type="button"
                  onclick={copyText}
                  aria-label={translate('open.copy.text')}
                  title={translate('open.copy.text')}
                  class="relative grid size-11 shrink-0 place-items-center rounded-[9px] bg-white/4 text-ink-soft ring-1 ring-white/10 hover:bg-white/8 hover:text-ink md:size-9"
                >
                  <span
                    class={[
                      'absolute inset-0 rounded-[9px] bg-aegis-500/20 shadow-[0_0_18px_-4px_oklch(0.56_0.19_258/0.8)] ring-1 ring-aegis-400/50 transition-opacity duration-500 ease-silk',
                      copied ? 'opacity-100' : 'opacity-0',
                    ]}
                  ></span>
                  <span class="relative">{@render swapIcons('copy', 'check', copied)}</span>
                </button>
                <span class="sr-only" role="status">{copied ? translate('open.copied.text') : ''}</span>
              {/if}
            </div>
            {#if revealText !== undefined}
              <pre
                class="max-h-[220px] min-h-0 [scrollbar-width:thin] overflow-auto rounded-pane px-5 py-4 font-sans text-[15px] leading-[1.75] break-words whitespace-pre-wrap text-ink well">{revealText}</pre>
            {/if}
            {#if revealed.file}
              {@const file = revealed.file}
              {@const extension = fileExtension(file.name)}
              {@const parts = splitFileName(file.name)}
              <div class="stack shrink-0 rounded-pane bg-white/3 py-2.5 pr-2.5 pl-3 ring-1 ring-white/8">
                <div
                  class={[
                    'flex items-center gap-3.5 fade-swap',
                    confirming ? 'pointer-events-none opacity-0' : 'opacity-100',
                  ]}
                  inert={confirming}
                >
                  <span
                    class="grid size-9 shrink-0 place-items-center rounded-[9px] bg-nebula-400/10 text-nebula-300 ring-1 ring-nebula-300/30"
                    aria-hidden="true"
                  >
                    <Icon name="file" stroke={1.7} class="size-[18px]" />
                  </span>
                  <span class="min-w-0 flex-1">
                    <bdi class="flex min-w-0 text-sm text-ink"
                      ><span class="truncate">{parts.stem}</span><span class="shrink-0">{parts.extension}</span></bdi
                    >
                    <span class="block font-mono text-[11px] text-ink-mute"
                      >{formatBytes(file.bytes.length, translator.language)}</span
                    >
                  </span>
                  <button
                    id="{uid}-download"
                    type="button"
                    disabled={confirming}
                    onclick={(event) => askDownload(event, file)}
                    class="btn-ghost flex h-11 shrink-0 hover-lift items-center gap-2 rounded-[9px] px-3 text-[13px] md:h-9"
                  >
                    {@render swapIcons('download', 'check', downloaded)}
                    {translate('open.download')}<span class="sr-only">{translate('open.download.suffix')}</span>
                  </button>
                </div>
                <div
                  class={[
                    'flex flex-col gap-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between',
                    confirming ? 'opacity-100' : 'pointer-events-none opacity-0',
                  ]}
                  inert={!confirming}
                >
                  <div class="flex min-w-0 items-center gap-3.5">
                    <span
                      class="grid size-9 shrink-0 place-items-center rounded-[9px] bg-rose-500/12 text-rose-300 ring-1 ring-rose-300/35"
                      aria-hidden="true"
                    >
                      <Icon name="alert" stroke={1.7} class="size-[18px]" />
                    </span>
                    <span class="min-w-0">
                      <span role="alert" class="block text-sm text-ink"
                        >{confirming ? translate('revealed.confirm.title') : ''}</span
                      >
                      <span class="block text-[11px] text-ink-mute"
                        >{extension
                          ? translate('revealed.confirm.extension', { extension })
                          : translate('revealed.confirm.none')}</span
                      >
                    </span>
                  </div>
                  <div class="grid shrink-0 grid-cols-2 gap-1.5 sm:flex">
                    <button
                      type="button"
                      disabled={!confirming}
                      onclick={(event) => confirmDownload(event, file)}
                      class="h-11 rounded-[9px] bg-rose-500/20 px-3 text-[13px] font-medium text-rose-100 ring-1 ring-rose-300/45 hover:bg-rose-500/30 md:h-9"
                    >
                      {translate('revealed.confirm.download')}
                    </button>
                    <button
                      id="{uid}-keep-file"
                      type="button"
                      onclick={cancelDownload}
                      class="btn-ghost h-11 rounded-[9px] px-3 text-[13px] md:h-9"
                    >
                      {translate('revealed.confirm.cancel')}
                    </button>
                  </div>
                </div>
              </div>
            {/if}
            <p class="flex shrink-0 items-center gap-2 px-1 text-[13px] text-ink-mute">
              <Icon name="eyeOff" class="size-4 shrink-0 text-nebula-300" />
              {translate('revealed.note')}
            </p>
          {/if}
        </div>

        <div
          class={[
            'flex flex-col items-center justify-center gap-7 px-5 py-12 text-center fade-swap sm:px-10 md:py-14',
            phase === 'notice' ? SHOWN : HIDDEN,
          ]}
          inert={phase !== 'notice'}
        >
          <span
            class={[
              'grid size-20 place-items-center rounded-[22px] ring-1',
              ALARMING.includes(notice)
                ? 'bg-rose-500/12 text-rose-300 ring-rose-300/35'
                : 'bg-white/4 text-ink-soft ring-white/12',
            ]}
            aria-hidden="true"
          >
            <Icon name={NOTICE_ICONS[notice]} stroke={1.6} class="size-9" />
          </span>
          <div>
            <h3 class="font-display text-2xl font-light tracking-[-0.02em] md:text-[28px]">{noticeTitle(notice)}</h3>
            <p class="mx-auto mt-2 max-w-[460px] text-[15px] leading-relaxed text-pretty text-ink-mute">
              {noticeText(notice)}
            </p>
          </div>
        </div>
      </div>

      <div class="relative border-t border-white/8">
        <div class="stack">
          <div
            id="{uid}-bar"
            class={[
              'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
              (phase === 'ready' || phase === 'opening') && !askingDelete
                ? 'opacity-100'
                : 'pointer-events-none opacity-0',
            ]}
            inert={!(phase === 'ready' || phase === 'opening') || askingDelete}
          >
            {@render chips(readyChips, phase === 'opening')}
            <div class="flex items-center">
              <div class="stack min-w-0 flex-1 sm:flex-none">
                <button
                  type="submit"
                  form="{uid}-form"
                  disabled={!ready || deleting || phase !== 'ready'}
                  class={[
                    'btn-primary flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-5 text-sm font-medium whitespace-nowrap transition-[opacity,translate,scale] duration-500 md:h-9 md:px-4',
                    phase === 'ready' ? 'opacity-100' : 'pointer-events-none opacity-0',
                  ]}
                >
                  <span class="sheen" aria-hidden="true"></span>
                  <Icon name="eye" stroke={2} class="size-4 shrink-0" />
                  {translate('read.reveal')}
                </button>
                <div
                  class={[
                    'relative flex h-11 items-center justify-center gap-2.5 overflow-hidden rounded-[9px] bg-aegis-950/70 px-5 text-sm font-medium whitespace-nowrap text-ink ring-1 ring-aegis-400/45 transition-opacity duration-500 md:h-9 md:px-4',
                    phase === 'opening' ? 'opacity-100' : 'pointer-events-none opacity-0',
                  ]}
                  aria-hidden="true"
                >
                  <progress
                    class="absolute inset-0 h-full w-full progress-fill"
                    max="1"
                    value={phase === 'opening' ? paced.progress : 0}
                  ></progress>
                  <span
                    class="absolute inset-y-0 left-0 w-1/3 animate-beam bg-linear-to-r from-transparent via-white/15 to-transparent"
                  ></span>
                  <svg class="relative size-4 shrink-0 animate-spin" viewBox="0 0 24 24" fill="none">
                    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-opacity="0.25" stroke-width="2.2"
                    ></circle>
                    <path
                      d="M20.5 12A8.5 8.5 0 0 0 12 3.5"
                      stroke="currentColor"
                      stroke-width="2.2"
                      stroke-linecap="round"
                    ></path>
                  </svg>
                  <span class="relative">{translate('open.opening')}</span>
                  <span
                    class="relative inline-block w-[4ch] font-mono text-[13px] whitespace-nowrap text-white/75 tabular-nums"
                    >{Math.round(paced.progress * 100)} %</span
                  >
                </div>
              </div>
            </div>
          </div>

          <div
            class={[
              'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
              (phase === 'revealed' || phase === 'notice') && !askingDelete
                ? 'opacity-100'
                : 'pointer-events-none opacity-0',
            ]}
            inert={!(phase === 'revealed' || phase === 'notice') || askingDelete}
          >
            {@render chips(afterChips)}
            {#if shown.view === 'failed' || shown.view === 'retry_later'}
              <button
                type="button"
                onclick={() => reader.load()}
                class="btn-primary flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-4 text-sm font-medium whitespace-nowrap sm:w-auto md:h-9"
              >
                <Icon name="refresh" class="size-4 shrink-0" />
                {translate('action.retry')}
              </button>
            {:else}
              <a
                href="/"
                class="btn-ghost flex h-11 w-full hover-lift items-center justify-center gap-2 rounded-[9px] px-4 text-[13px] whitespace-nowrap sm:w-auto md:h-9"
              >
                <Icon name="pen" class="size-4 shrink-0" />
                {translate('nav.write')}
              </a>
            {/if}
          </div>

          <div
            role="alertdialog"
            tabindex="-1"
            aria-labelledby="{uid}-confirm"
            onkeydown={onconfirmkey}
            class={[
              'flex flex-col gap-2 p-2.5 fade-swap sm:flex-row sm:items-center sm:justify-between sm:gap-3',
              askingDelete ? 'opacity-100' : 'pointer-events-none opacity-0',
            ]}
            inert={!askingDelete}
          >
            <p id="{uid}-confirm" class="flex items-center gap-2 px-1.5 text-[13px] text-ink-soft">
              <Icon name="trash" class="size-4 shrink-0 text-rose-300" />
              {translate('read.delete.confirm')}
            </p>
            <div class="grid grid-cols-2 gap-1.5 sm:flex sm:items-center">
              <button
                id="{uid}-keep"
                type="button"
                onclick={cancelDelete}
                class="btn-ghost flex h-11 hover-lift items-center justify-center rounded-[9px] px-4 text-[13px] whitespace-nowrap md:h-9"
              >
                {translate('read.delete.no')}
              </button>
              <button
                type="button"
                disabled={!askingDelete}
                onclick={burn}
                class="flex h-11 hover-lift items-center justify-center gap-2 rounded-[9px] bg-rose-500/20 px-4 text-[13px] font-medium whitespace-nowrap text-rose-100 ring-1 ring-rose-300/45 hover:bg-rose-500/30 md:h-9"
              >
                {translate('read.delete.yes')}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div
    class="relative z-30 mx-auto mt-6 flex max-w-[860px] flex-col-reverse items-center gap-4 px-1 sm:flex-row sm:justify-between"
  >
    <div class="stack text-xs">
      <p
        class={[
          'flex items-center justify-center gap-2 text-center text-ink-mute fade-swap',
          burnNote ? 'opacity-0' : 'opacity-100',
        ]}
      >
        <Icon name="shieldCheck" stroke={1.7} class="size-4 shrink-0 text-aegis-300" />
        {translate('compose.note')}
      </p>
      <p
        class={[
          'flex items-center justify-center gap-2 text-center fade-swap',
          burnFailed ? 'text-rose-300' : 'text-ink-soft',
          burnNote ? 'opacity-100' : 'opacity-0',
        ]}
      >
        <Icon name={burnFailed ? 'alert' : 'check'} stroke={1.7} class="size-4 shrink-0" />
        {burnNote}
      </p>
    </div>
    <div
      class={[
        'flex items-center gap-1 transition-opacity duration-500',
        actions === 'on'
          ? 'opacity-100'
          : actions === 'dim'
            ? 'pointer-events-none opacity-40'
            : 'pointer-events-none opacity-0',
      ]}
      inert={actions !== 'on'}
    >
      <div class="stack justify-items-end">
        {#each deleteActions as action (action.id)}
          <button
            id="{uid}-{action.id}"
            type="button"
            disabled={!action.enabled}
            onclick={askDelete}
            inert={!action.shown}
            class={[
              'flex h-11 items-center gap-2 rounded-[9px] px-3 text-[13px] whitespace-nowrap text-ink-mute transition-opacity duration-300 hover:bg-rose-500/10 hover:text-rose-200 disabled:hover:bg-transparent disabled:hover:text-ink-mute md:h-9',
              action.shown ? 'opacity-100' : 'pointer-events-none opacity-0',
            ]}
          >
            <Icon name="trash" class="size-4 shrink-0" />
            {translate(action.label)}
          </button>
        {/each}
      </div>
      <div
        class={[
          'relative transition-opacity duration-300',
          reportable ? 'opacity-100' : 'pointer-events-none opacity-0',
        ]}
        inert={!reportable}
        data-menu
      >
        <button
          id="{uid}-report-button"
          type="button"
          aria-haspopup="dialog"
          aria-expanded={reporting}
          aria-controls="{uid}-report"
          onclick={toggleReport}
          class={[
            'flex h-11 items-center gap-2 rounded-[9px] px-3 text-[13px] md:h-9',
            reporting ? 'bg-white/8 text-ink' : 'text-ink-mute hover:bg-white/5 hover:text-ink',
          ]}
        >
          <Icon name="flag" class="size-4 shrink-0" />
          {translate('open.report')}
        </button>
        <Popover
          id="{uid}-report"
          label={translate('open.report.title')}
          open={reporting}
          from="below"
          onclose={closeReport}
          class="top-full right-0 mt-2 w-80 max-w-[calc(100vw-2rem)] p-3.5 text-left"
        >
          <div class="stack">
            <div class={['fade-swap', reportSent ? 'pointer-events-none opacity-0' : 'opacity-100']} inert={reportSent}>
              <p class="px-0.5 text-[13px] text-ink">{translate('open.report.title')}</p>
              <p class="mt-1 px-0.5 text-[11px] leading-relaxed text-ink-mute">{translate('open.report.note')}</p>
              {#key reportRound}
                <ReportForm
                  name="{uid}-report-form"
                  compact
                  hint={translate('open.report.privacy')}
                  send={(_link, reason) => reader.report(reason)}
                  onaccepted={reportAccepted}
                />
              {/key}
            </div>
            <div
              class={[
                'flex flex-col items-center justify-center gap-2 text-center fade-swap',
                reportSent ? 'opacity-100' : 'pointer-events-none opacity-0',
              ]}
              inert={!reportSent}
            >
              <span
                class="grid size-10 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 ring-1 ring-aegis-400/35"
                aria-hidden="true"
              >
                <Icon name="check" stroke={2} class="size-5" />
              </span>
              <p class="text-[13px] text-ink">{translate('open.report.sent')}</p>
              <p class="text-[11px] text-ink-mute">{translate('open.report.sent.note')}</p>
            </div>
          </div>
        </Popover>
      </div>
    </div>
  </div>
</section>

<section class="px-4 pb-28 sm:px-6 md:pb-36" aria-labelledby="{uid}-trust">
  <div class="mx-auto max-w-[1160px]">
    <h2 id="{uid}-trust" class="reveal-up text-center text-sm font-medium text-aegis-300">
      {translate('open.trust.title')}
    </h2>
    <ol class="mt-8 grid gap-4 md:grid-cols-3 md:gap-6">
      {#each TRUST as card (card.key)}
        <li
          class={[
            'glow-on-hover glass flex hover-lift items-start gap-4 rounded-card p-5 text-left md:flex-col md:items-center md:gap-0 md:px-6 md:pt-7 md:pb-8 md:text-center',
            card.reveal,
          ]}
        >
          <span
            class="grid size-12 shrink-0 place-items-center rounded-control bg-aegis-500/15 text-aegis-200 ring-1 ring-aegis-400/35"
          >
            <Icon name={card.icon} stroke={1.6} class="size-6" />
          </span>
          <div class="min-w-0">
            <h3 class="text-[17px] font-medium md:mt-6 md:text-lg">{translate(`${card.key}.title`)}</h3>
            <p class="mt-1 max-w-[300px] text-[15px] leading-relaxed text-pretty text-ink-soft md:mx-auto md:mt-2">
              {translate(`${card.key}.text`)}
            </p>
          </div>
        </li>
      {/each}
    </ol>
  </div>
</section>
