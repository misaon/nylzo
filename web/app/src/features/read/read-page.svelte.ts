import { KDF_NONE } from '@nylzo/core';
import { flushSync, onDestroy, tick } from 'svelte';
import { prefersReducedMotion } from 'svelte/motion';

import type { Metadata } from '../../shared/api/api.ts';
import { createApi } from '../../shared/api/api.ts';
import { clearClipboard } from '../../shared/clipboard/clear-clipboard.ts';
import { isKnownSafeFile } from '../../shared/files/file-name.ts';
import { saveFile } from '../../shared/files/save-file.ts';
import { scryptInWorker, stopRunningWorkers } from '../../shared/kdf/kdf.ts';
import { prepareWorker } from '../../shared/kdf/worker-source.ts';
import type { MessageKey } from '../../shared/language/czech.ts';
import { formatMoment, formatWait } from '../../shared/language/format.ts';
import type { Translator } from '../../shared/language/translator.svelte.ts';
import { takeFragment } from '../../shared/link/link-handoff.ts';
import { PacedTimeline } from '../../shared/pipeline/paced-timeline.svelte.ts';
import { randomCharacter, randomText } from '../../shared/pipeline/random-text.ts';
import { FocusTargets, keepFocus } from '../../shared/ui/focus/focus.ts';
import type { IconName } from '../../shared/ui/icon/icons.ts';
import { Flash } from '../../shared/ui/transition/flash.svelte.ts';
import { noticeOf } from './read-notice.ts';
import type { Burned, Notice } from './read-notice.ts';
import { createReader } from './reader.ts';
import type { BurnOutcome, BusyStep, Reader, ReadView, RevealedFile } from './reader.ts';
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
const FAILED_BURNS: ReadonlySet<BurnOutcome['outcome']> = new Set(['failed', 'retry_later', 'unknown']);
const WHITESPACE: ReadonlySet<string> = new Set([' ', '\n', '\t']);

function scramble(characters: readonly string[], progress: number): string {
  const cut = Math.floor(characters.length * Math.min(1, progress * SCRAMBLE_LEAD));
  return characters
    .map((character, index) => (index < cut || WHITESPACE.has(character) ? character : randomCharacter()))
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
  readonly burnNote = $derived(this.burned ? this.#burnText(this.burned) : '');
  readonly burnFailed = $derived(this.burned !== undefined && FAILED_BURNS.has(this.burned.outcome.outcome));

  readonly actions = $derived.by(() => {
    if (this.phase === 'opening') {
      return 'dim';
    }
    const actionable = this.phase === 'ready' || this.phase === 'revealed' || this.locked;
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
      return [
        { icon: 'check', text: translate('open.chip.now') },
        { icon: 'eye', text: more ? this.opensLeft(this.revealed.opensRemaining) : translate('open.chip.last') },
      ];
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
    this.burned = undefined;
    this.askingDelete = true;
    await this.focus.focusAfterUpdate(() => 'keepMessage');
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
    await this.focus.focusAfterUpdate(() => 'keepFile');
  };

  readonly burn = async (): Promise<void> => {
    this.askingDelete = false;
    this.burned = undefined;
    this.deleting = true;
    const afterReveal = this.shown.view === 'revealed';
    const outcome = await this.#reader.burn();
    this.deleting = false;
    this.restDeleted ||= afterReveal && (outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    this.burned = { outcome, afterReveal };
    await tick();
    keepFocus(afterReveal ? this.focus.get('revealedTitle') : this.focus.get('readTitle'));
  };

  readonly cancelDelete = async (): Promise<void> => {
    this.askingDelete = false;
    await this.focus.focusAfterUpdate(() => (this.revealed ? 'deleteRest' : 'deleteMessage'));
  };

  readonly cancelDownload = async (event: MouseEvent): Promise<void> => {
    if (event.detail > 1) {
      return;
    }
    this.confirmingDownload = false;
    await this.focus.focusAfterUpdate(() => 'download');
  };

  readonly confirmDownload = async (event: MouseEvent, file: RevealedFile): Promise<void> => {
    if (event.detail > 1 || !this.confirmingDownload) {
      return;
    }
    this.#save(file);
    await this.focus.focusAfterUpdate(() => 'download');
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

  readonly clearClipboard = (): Promise<void> =>
    clearClipboard((state) => {
      this.clipboard = state;
    });

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
    stopRunningWorkers();
    this.#left = true;
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
    this.#reader.clear();
    this.#settleShown();
    // Commit removal before pagehide returns and the browser freezes this document.
    flushSync();
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

  readonly reveal = (event: SubmitEvent): void => {
    event.preventDefault();
    if (this.shown.view === 'loading') {
      this.#revealWhenReady = true;
      return;
    }
    this.#submit();
  };

  #submit(): void {
    if (this.#revealing || this.phase !== 'ready' || this.shown.view !== 'ready' || this.deleting) {
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
        session: {
          getItem: (key) => sessionStorage.getItem(key),
          setItem: (key, value) => sessionStorage.setItem(key, value),
          removeItem: (key) => sessionStorage.removeItem(key),
        },
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
    onDestroy(() => {
      this.timeline.stop();
      clearInterval(this.#revealTimer);
      this.#reader.clear();
    });
    void this.#reader.load();
  }

  #burnText({ outcome, afterReveal }: Burned): string {
    const { translate, language } = this.#translator;
    switch (outcome.outcome) {
      case 'deleted': {
        return afterReveal ? translate('read.deleted_rest') : '';
      }
      case 'ended': {
        return afterReveal ? translate(`read.ended.${outcome.state}`) : '';
      }
      case 'retry_later': {
        return translate('read.retry_later', { time: formatWait(outcome.retryAfter, language) });
      }
      case 'unknown': {
        return afterReveal ? translate('read.delete.failed') : '';
      }
      case 'failed': {
        return translate('read.delete.failed');
      }
      case 'ignored': {
        return '';
      }
    }
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
      void this.focus.focusAfterUpdate(() => 'passphrase');
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

  #show(next: ReadView): void {
    const wasRevealed = this.shown.view === 'revealed';
    this.shown = next;
    if (next.view !== 'revealed') {
      clearInterval(this.#revealTimer);
      this.revealText = undefined;
      this.marked = undefined;
      return;
    }
    this.#settleShown();
    if (wasRevealed) {
      return;
    }

    void this.#startReveal(next.text);
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
    await this.focus.focusAfterUpdate(() => 'revealedTitle');
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
    const { translate, language } = this.#translator;
    const rule = new Intl.PluralRules(language).select(count);
    const keys: Partial<Record<Intl.LDMLPluralRule, MessageKey>> = {
      one: 'open.chip.opens.one',
      few: 'open.chip.opens.few',
    };
    return translate(keys[rule] ?? 'open.chip.opens.other', { count });
  }

  shortTime(timestamp: string): string {
    return formatMoment(timestamp, this.#translator.language);
  }
}
