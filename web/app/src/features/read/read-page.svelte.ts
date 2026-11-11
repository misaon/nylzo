import { KDF_NONE } from '@nylzo/core';
import { flushSync, onDestroy } from 'svelte';
import { prefersReducedMotion } from 'svelte/motion';

import type { Metadata } from '../../shared/api/api.ts';
import { createApi } from '../../shared/api/api.ts';
import { clearClipboard } from '../../shared/clipboard/clear-clipboard.ts';
import { isKnownSafeFile } from '../../shared/files/file-name.ts';
import { revokeSavedFiles, saveFile } from '../../shared/files/save-file.ts';
import { scryptInWorker, stopRunningWorkers } from '../../shared/kdf/kdf.ts';
import { prepareWorker } from '../../shared/kdf/worker-source.ts';
import { formatClock, formatMoment } from '../../shared/language/format.ts';
import type { Translator } from '../../shared/language/translator.svelte.ts';
import { takeFragment } from '../../shared/link/link-handoff.ts';
import { PacedTimeline } from '../../shared/pipeline/paced-timeline.svelte.ts';
import { randomText } from '../../shared/pipeline/random-text.ts';
import { FocusTargets } from '../../shared/ui/focus/focus.ts';
import type { IconName } from '../../shared/ui/icon/icons.ts';
import { Flash } from '../../shared/ui/transition/flash.svelte.ts';
import { burnText, noticeOf } from './read-notice.ts';
import type { Burned, Notice } from './read-notice.ts';
import { readerSession } from './reader-session.ts';
import { createReader } from './reader.ts';
import type { BurnOutcome, BusyStep, ClosedView, Reader, ReadView, RevealedFile } from './reader.ts';
import { markHiddenCharacters } from './revealed/hidden-characters.ts';
import type { MarkedText } from './revealed/hidden-characters.ts';

export type Phase = 'notice' | 'opening' | 'ready' | 'revealed';

export interface Fact {
  readonly icon: IconName;
  readonly text: string;
  readonly accent?: boolean;
}

export type FocusTarget =
  | 'bar'
  | 'clipboard'
  | 'deleteMessage'
  | 'deleteRest'
  | 'download'
  | 'keepFile'
  | 'keepMessage'
  | 'passphrase'
  | 'readTitle'
  | 'readyArea'
  | 'revealedTitle';

export function panelClasses(shown: boolean, phase: Phase): string {
  if (shown) {
    return 'relative translate-y-0 opacity-100';
  }
  return phase === 'revealed'
    ? 'pointer-events-none absolute inset-x-0 top-0 translate-y-3 opacity-0'
    : 'pointer-events-none relative translate-y-3 opacity-0';
}

const STEPS = [
  { id: 'derive', busy: 'deriving', icon: 'key' },
  { id: 'claim', busy: 'claiming', icon: 'envelopeOpen' },
  { id: 'download', busy: 'downloading', icon: 'download' },
  { id: 'decrypt', busy: 'decrypting', icon: 'unlock' },
] as const satisfies readonly { readonly id: string; readonly busy: BusyStep; readonly icon: IconName }[];

const MIN_MS = { passphrase: 3800, plain: 2600, reduced: 1000 } as const;
const REVEAL_MS = 900;
const REVEAL_TICK_MS = 40;
const SCRAMBLED_CHARACTERS = 2000;
const SCRAMBLE_LEAD = 1.15;
const FLASH_MS = 2400;
const NOISE_CHARACTERS = 260;
const NOISE_GROWTH = 900;
const SETTLE_WAIT_MS = 700;
const COUNTDOWN_TICK_MS = 1000;
export const SHOWN_SECONDS = 15 * 60;
const FAILED_BURNS: ReadonlySet<BurnOutcome['outcome']> = new Set(['failed', 'retry_later', 'unknown', 'waiting']);
const UNSENT_BURNS: ReadonlySet<BurnOutcome['outcome']> = new Set(['failed', 'ignored', 'retry_later']);
const WHITESPACE: ReadonlySet<string> = new Set([' ', '\n', '\t']);

function scramble(characters: readonly string[], progress: number): string {
  const cut = Math.floor(characters.length * Math.min(1, progress * SCRAMBLE_LEAD));
  const noise = randomText(characters.length);
  return characters
    .map((character, index) => (index < cut || WHITESPACE.has(character) ? character : noise.charAt(index)))
    .join('');
}

function charactersOf(text: string): readonly string[] {
  return text.match(/./gsu) ?? [];
}

export class ReadPage {
  #left = false;
  #pending: ReadView | undefined;
  #revealing = false;
  #revealWhenReady = false;
  #markShown: (() => void) | undefined;
  #revealTimer: ReturnType<typeof setInterval> | undefined;
  #countdownTimer: ReturnType<typeof setInterval> | undefined;
  readonly #settleTimer: ReturnType<typeof setTimeout>;
  #shownUntil = 0;
  readonly #reader: Reader;
  readonly #translator: Translator;

  view = $state.raw<ReadView>({ view: 'loading' });
  shown = $state.raw<ReadView>({ view: 'loading' });
  passphrase = $state('');
  showPassphrase = $state(false);
  emptyPassphrase = $state(false);
  typedAgain = $state(false);
  passphraseRequired = $state(false);
  metadata = $state.raw<Metadata>();
  confirmingDownload = $state(false);
  askingDelete = $state(false);
  deleteIntent = $state(false);
  restDeleted = $state(false);
  deleting = $state(false);
  burned = $state.raw<Burned>();
  opening = $state(false);
  revealText = $state<string>();
  marked = $state.raw<MarkedText>();
  confirmingCopy = $state(false);
  clipboard = $state<'cleared' | 'copied' | 'untouched'>('untouched');
  linkInHistory = $state(false);
  noise = $state('');
  settled = $state(false);
  secondsLeft = $state<number>();
  readonly copied = new Flash(FLASH_MS);
  readonly downloaded = new Flash(FLASH_MS);
  readonly focus = new FocusTargets<FocusTarget>();
  readonly steps = $derived(this.passphraseRequired ? STEPS : STEPS.slice(1));

  readonly minMs = $derived.by(() => {
    if (prefersReducedMotion.current) {
      return MIN_MS.reduced;
    }
    return this.passphraseRequired ? MIN_MS.passphrase : MIN_MS.plain;
  });

  readonly timeline = new PacedTimeline(() => ({ minMs: this.minMs, steps: this.steps.length }));
  readonly ready = $derived(this.shown.view === 'ready' ? this.shown : undefined);
  readonly revealed = $derived(this.shown.view === 'revealed' ? this.shown : undefined);
  readonly locked = $derived(this.ready?.rejected?.reason === 'wrong' && this.ready.rejected.locked);

  readonly phase: Phase = $derived.by(() => {
    if (this.opening) {
      return 'opening';
    }
    if (this.revealed) {
      return 'revealed';
    }
    return (this.ready && !this.locked) || this.shown.view === 'loading' ? 'ready' : 'notice';
  });

  readonly notice: Notice = $derived(noticeOf(this.shown, this.burned));
  readonly burnNote = $derived(this.#burnNote());
  readonly burnFailed = $derived(this.burned !== undefined && FAILED_BURNS.has(this.burned.outcome.outcome));

  readonly actions = $derived.by(() => {
    if (this.phase === 'opening') {
      return 'dim';
    }
    const actionable = this.phase === 'ready' || this.phase === 'revealed';
    return actionable && this.shown.view !== 'loading' && !this.askingDelete ? 'on' : 'off';
  });

  readonly status = $derived.by(() => {
    const { translate } = this.#translator;
    if (this.shown.view === 'loading') {
      return translate('read.loading');
    }
    if (this.opening && this.view.view === 'busy') {
      return translate(`busy.${this.view.step}`);
    }
    if (this.notice === 'timed_out') {
      return translate('open.notice.timed_out');
    }
    return this.burnNote;
  });

  readonly readyFacts: readonly Fact[] = $derived.by(() => {
    const { translate } = this.#translator;
    if (!this.metadata) {
      return [];
    }
    const facts: Fact[] = [];
    if (this.metadata.expiresAt) {
      facts.push({
        icon: 'clock',
        text: translate('open.chip.expires', { time: this.shortTime(this.metadata.expiresAt) }),
      });
    }
    facts.push({ icon: 'eye', text: this.opensLeft(this.metadata.opensRemaining) });
    if (this.metadata.kdfId !== KDF_NONE) {
      facts.push({ icon: 'key', text: translate('create.passphrase'), accent: true });
    }
    return facts;
  });

  readonly afterFacts: readonly Fact[] = $derived.by(() => {
    const { translate } = this.#translator;
    if (this.revealed) {
      const more = this.revealed.burnable && !this.restDeleted;
      const facts: Fact[] = [
        { icon: 'check', text: translate('open.chip.now') },
        { icon: 'eye', text: more ? this.opensLeft(this.revealed.opensRemaining) : translate('open.chip.last') },
      ];
      if (this.secondsLeft !== undefined) {
        facts.unshift({ icon: 'timer', text: translate('open.chip.clears', { time: formatClock(this.secondsLeft) }) });
      }
      return facts;
    }
    if (this.notice === 'burned') {
      return [{ icon: 'check', text: translate('open.chip.burned') }];
    }
    if (this.shown.view === 'ended' && this.shown.firstOpenedAt) {
      return [
        { icon: 'eye', text: translate('open.chip.first_opened', { time: this.shortTime(this.shown.firstOpenedAt) }) },
      ];
    }
    if (this.shown.view === 'ended' && this.shown.endedAt) {
      return [{ icon: 'clock', text: translate('open.chip.ended', { time: this.shortTime(this.shown.endedAt) }) }];
    }
    return [];
  });

  readonly askDelete = async (): Promise<void> => {
    if (this.deleting) {
      return;
    }
    this.burned = undefined;
    this.deleteIntent = this.ready !== undefined && this.passphraseRequired;
    if (this.#missingPassphrase()) {
      return;
    }
    this.askingDelete = true;
    await this.focus.focusAfterUpdate('keepMessage');
  };

  readonly askDownload = async (event: MouseEvent, file: RevealedFile): Promise<void> => {
    if (event.detail > 1) {
      return;
    }
    if (isKnownSafeFile(file.name)) {
      this.#save(file);
      return;
    }
    this.confirmingDownload = true;
    await this.focus.focusAfterUpdate('keepFile');
  };

  readonly burn = async (): Promise<void> => {
    this.askingDelete = false;
    if (this.#missingPassphrase()) {
      return;
    }
    this.burned = undefined;
    this.deleting = true;
    const afterReveal = this.shown.view === 'revealed';
    const outcome = await this.#reader.burn(afterReveal ? '' : this.passphrase);
    this.deleting = false;
    if (!afterReveal && !UNSENT_BURNS.has(outcome.outcome)) {
      this.passphrase = '';
      this.showPassphrase = false;
    }
    this.deleteIntent &&= outcome.outcome === 'rejected' || UNSENT_BURNS.has(outcome.outcome);
    this.restDeleted ||= afterReveal && (outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    this.burned = { outcome, afterReveal };
    if (outcome.outcome === 'rejected') {
      await this.focus.focusAfterUpdate('passphrase');
      return;
    }
    await this.focus.keepFocusAfterUpdate(afterReveal ? 'revealedTitle' : 'readTitle');
  };

  readonly cancelDelete = async (): Promise<void> => {
    this.askingDelete = false;
    this.deleteIntent = false;
    await this.focus.focusAfterUpdate(() => (this.revealed ? 'deleteRest' : 'deleteMessage'));
  };

  readonly cancelDownload = async (event: MouseEvent): Promise<void> => {
    if (event.detail > 1) {
      return;
    }
    this.confirmingDownload = false;
    await this.focus.focusAfterUpdate('download');
  };

  readonly confirmDownload = async (event: MouseEvent, file: RevealedFile): Promise<void> => {
    if (event.detail > 1 || !this.confirmingDownload) {
      return;
    }
    this.#save(file);
    await this.focus.focusAfterUpdate('download');
  };

  readonly askCopy = async (): Promise<void> => {
    if ((this.marked?.hidden ?? 0) === 0) {
      await this.copyText();
      return;
    }
    this.confirmingCopy = true;
  };

  readonly cancelCopy = (): void => {
    this.confirmingCopy = false;
  };

  readonly clearClipboard = async (): Promise<void> => {
    await clearClipboard((state) => {
      this.clipboard = state;
    });
    await this.focus.keepFocusAfterUpdate('clipboard');
  };

  readonly copyText = async (): Promise<void> => {
    this.confirmingCopy = false;
    if (!this.revealed?.text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(this.revealed.text);
      this.clipboard = 'copied';
      this.copied.show();
    } catch {
      this.copied.hide();
    }
  };

  readonly forgetPassphrase = (): void => {
    this.passphrase = '';
    this.showPassphrase = false;
    flushSync();
  };

  readonly leave = (): void => {
    this.#left = true;
    this.#close({ view: 'cleared' });
    // Commit removal before pagehide returns and the browser freezes this document.
    flushSync();
  };

  readonly checkCountdown = (): void => {
    if (this.#countdownTimer === undefined) {
      return;
    }
    const left = (this.#shownUntil - performance.now()) / 1000;
    if (left > 0) {
      this.secondsLeft = Math.ceil(left);
      return;
    }
    this.#close({ view: 'timed_out' });
  };

  readonly onConfirmKeydown = (event: KeyboardEvent): void => {
    if (event.key !== 'Escape') {
      return;
    }

    event.stopPropagation();
    void this.cancelDelete();
  };

  readonly retry = (): void => {
    void this.#reader.load();
  };

  readonly returned = (event: PageTransitionEvent): void => {
    if (event.persisted || this.#left) {
      location.reload();
    }
  };

  // The page drops the fragment on load, so a link pasted again into the address bar arrives without a new load.
  readonly fragmentArrived = (): void => {
    if (location.hash !== '') {
      location.reload();
    }
  };

  // Enter in the passphrase field asked for to delete must not open the message; only the reveal button does then.
  readonly reveal = (event: SubmitEvent): void => {
    event.preventDefault();
    if (this.deleteIntent && !event.submitter) {
      void this.askDelete();
      return;
    }
    if (this.shown.view === 'loading') {
      this.#revealWhenReady = true;
      return;
    }
    this.#submit();
  };

  #submit(): void {
    if (
      this.#revealing ||
      this.phase !== 'ready' ||
      this.shown.view !== 'ready' ||
      this.deleting ||
      this.askingDelete
    ) {
      return;
    }
    if (this.passphraseRequired && this.passphrase === '') {
      this.emptyPassphrase = true;
      this.focus.focus('passphrase');
      return;
    }
    this.emptyPassphrase = false;
    this.showPassphrase = false;
    this.askingDelete = false;
    this.deleteIntent = false;
    this.burned = undefined;
    const entered = this.passphrase;
    this.passphrase = '';
    this.#revealing = true;
    void this.#revealWith(entered);
  }

  readonly typed = (): void => {
    this.emptyPassphrase = false;
    this.typedAgain = true;
  };

  constructor(translator: Translator) {
    this.#translator = translator;
    this.#reader = createReader(
      {
        api: createApi(),
        location,
        takeFragment: () => {
          const taken = takeFragment();
          this.linkInHistory = taken.fragment !== '' && !taken.handedOver;
          return taken.fragment;
        },
        session: readerSession(),
        page: {
          visible: () => document.visibilityState === 'visible',
          prerendering: () => document.prerendering === true,
        },
        prepared: prepareWorker,
        shown: async () => this.#displayed(),
        scrypt: scryptInWorker,
        now: () => Date.now(),
      },
      (view) => this.#onView(view),
    );
    this.#settleTimer = setTimeout(() => {
      this.settled = true;
    }, SETTLE_WAIT_MS);
    onDestroy(() => {
      this.timeline.stop();
      clearInterval(this.#revealTimer);
      clearTimeout(this.#settleTimer);
      this.#stopCountdown();
      this.#reader.clear();
    });
    void this.#reader.load();
  }

  // An empty passphrase would cost a shared attempt, so a delete waits for one.
  #missingPassphrase(): boolean {
    if (!this.ready || !this.passphraseRequired || this.passphrase !== '') {
      return false;
    }
    this.emptyPassphrase = true;
    this.focus.focus('passphrase');
    return true;
  }

  #burnNote(): string {
    if (this.burned) {
      return burnText(this.burned, this.#translator);
    }
    return this.deleting && this.ready && this.passphraseRequired ? this.#translator.translate('busy.deriving') : '';
  }

  async #displayed(): Promise<void> {
    return new Promise((resolve) => {
      if (this.shown.view === 'revealed') {
        resolve();
      } else {
        this.#markShown = resolve;
      }
    });
  }

  #onView(next: ReadView): void {
    const wasOpening = this.opening;
    this.view = next;
    if (next.view === 'loading' && next.kdfId !== undefined) {
      this.passphraseRequired = next.kdfId !== KDF_NONE;
    }
    if (next.view === 'ready') {
      this.passphraseRequired = next.metadata.kdfId !== KDF_NONE;
      this.metadata = next.metadata;
      this.typedAgain = false;
      this.emptyPassphrase = false;
    } else if (next.view === 'busy') {
      if (!this.opening) {
        this.#startTimeline();
      }
      this.timeline.reached = this.#stepIndex(next.step);
      this.timeline.fraction = next.fraction;
      return;
    }
    if (next.view === 'revealed' && this.opening) {
      this.#pending = next;
      this.timeline.complete();
      return;
    }
    this.#pending = undefined;
    this.#stopTimeline();
    this.#show(next);
    const submitted = this.#revealWhenReady && next.view === 'ready';
    if (next.view !== 'loading') {
      this.#revealWhenReady = false;
    }
    if (submitted) {
      this.#submit();
      return;
    }
    if (wasOpening && next.view === 'ready' && this.passphraseRequired) {
      void this.focus.focusAfterUpdate('passphrase');
    }
  }

  async #revealWith(passphrase: string): Promise<void> {
    try {
      await this.#reader.reveal(passphrase);
    } finally {
      this.#revealing = false;
    }
  }

  #save(file: RevealedFile): void {
    this.confirmingDownload = false;
    saveFile(file.bytes, file.name);
    this.downloaded.show();
  }

  #settleShown(): void {
    this.#markShown?.();
    this.#markShown = undefined;
  }

  #close(closed: ClosedView): void {
    stopRunningWorkers();
    revokeSavedFiles();
    this.burned = undefined;
    this.confirmingDownload = false;
    this.askingDelete = false;
    this.showPassphrase = false;
    this.passphrase = '';
    this.#pending = undefined;
    this.revealText = undefined;
    this.marked = undefined;
    this.confirmingCopy = false;
    clearInterval(this.#revealTimer);
    this.#stopTimeline();
    this.#stopCountdown();
    this.#reader.clear(closed);
    this.#settleShown();
  }

  #show(next: ReadView): void {
    const wasRevealed = this.shown.view === 'revealed';
    this.shown = next;
    if (next.view !== 'loading') {
      clearTimeout(this.#settleTimer);
      this.settled = true;
    }
    if (next.view !== 'revealed') {
      clearInterval(this.#revealTimer);
      this.#stopCountdown();
      this.revealText = undefined;
      this.marked = undefined;
      return;
    }
    this.#settleShown();
    if (wasRevealed) {
      return;
    }

    this.#startCountdown();
    void this.#startReveal(next.text);
  }

  #startCountdown(): void {
    this.#stopCountdown();
    this.#shownUntil = performance.now() + SHOWN_SECONDS * 1000;
    this.secondsLeft = SHOWN_SECONDS;
    this.#countdownTimer = setInterval(this.checkCountdown, COUNTDOWN_TICK_MS);
  }

  #stopCountdown(): void {
    clearInterval(this.#countdownTimer);
    this.#countdownTimer = undefined;
    this.secondsLeft = undefined;
  }

  async #startReveal(revealed: string | undefined): Promise<void> {
    clearInterval(this.#revealTimer);
    this.marked = revealed === undefined ? undefined : markHiddenCharacters(revealed);
    const text = this.marked?.shown;
    this.revealText = text;
    if (text !== undefined && !prefersReducedMotion.current) {
      const started = performance.now();
      const characters = charactersOf(text.slice(0, SCRAMBLED_CHARACTERS));
      const rest = text.slice(characters.join('').length);
      this.revealText = scramble(characters, 0) + rest;
      this.#revealTimer = setInterval(() => {
        const progress = Math.min(1, (performance.now() - started) / REVEAL_MS);
        this.revealText = progress >= 1 ? text : scramble(characters, progress) + rest;
        if (progress >= 1) {
          clearInterval(this.#revealTimer);
        }
      }, REVEAL_TICK_MS);
    }
    await this.focus.focusAfterUpdate('revealedTitle');
  }

  #startTimeline(): void {
    this.opening = true;
    this.noise = randomText(NOISE_CHARACTERS);
    const active = document.activeElement;
    if (active && [this.focus.get('readyArea'), this.focus.get('bar')].some((element) => element?.contains(active))) {
      this.focus.focus('readTitle');
    }
    this.timeline.start(() => {
      this.noise = randomText(NOISE_CHARACTERS + Math.floor(this.timeline.paced.progress * NOISE_GROWTH));
      const next = this.#pending;
      if (!next || !this.timeline.paced.finished) {
        return;
      }
      this.#pending = undefined;
      this.#stopTimeline();
      this.#show(next);
    });
  }

  #stepIndex(step: BusyStep): number {
    return Math.max(
      0,
      this.steps.findIndex((item) => item.busy === step),
    );
  }

  #stopTimeline(): void {
    this.timeline.stop();
    this.opening = false;
  }

  opensLeft(count: number): string {
    return this.#translator.plural('open.chip.opens', count);
  }

  shortTime(timestamp: string): string {
    return formatMoment(timestamp, this.#translator.language);
  }
}
