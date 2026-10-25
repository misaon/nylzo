import { onDestroy, tick } from 'svelte';
import { prefersReducedMotion } from 'svelte/motion';

import type { Metadata, ReportStatus } from '../../shared/api/api.ts';
import type { MessageKey } from '../../shared/language/czech.ts';
import type { Translator } from '../../shared/language/translator.svelte.ts';
import type { ReportOutcome, ReportSubmission } from '../../shared/report/report.ts';
import type { IconName } from '../../shared/ui/icons.ts';

import { createApi } from '../../shared/api/api.ts';
import { isKnownSafeFile } from '../../shared/files/file-name.ts';
import { scryptInWorker } from '../../shared/kdf/kdf.ts';
import { formatWait } from '../../shared/language/format.ts';
import { PacedTimeline } from '../../shared/pipeline/paced-timeline.svelte.ts';
import { randomCharacter, randomText } from '../../shared/pipeline/random-text.ts';
import { Flash } from '../../shared/ui/flash.svelte.ts';
import { FocusTargets, keepFocus } from '../../shared/ui/focus.ts';
import { type Burned, type Notice, noticeOf } from './read-notice.ts';
import {
  type BurnOutcome,
  type BusyStep,
  createReader,
  type Reader,
  type ReadView,
  type RevealedFile,
} from './reader.ts';
import { saveFile } from './save-file.ts';

export type Phase = 'notice' | 'opening' | 'ready' | 'revealed';

export interface ChipData {
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
  | 'reportButton'
  | 'reportPopover'
  | 'revealedTitle';

export const PANEL_SHOWN = 'translate-y-0 opacity-100';
export const PANEL_HIDDEN = 'pointer-events-none translate-y-3 opacity-0';

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
  #pending: ReadView | undefined;
  #revealing = false;
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
  reporting = $state(false);
  reportSent = $state<ReportStatus>();
  reportRound = $state(0);
  restDeleted = $state(false);
  deleting = $state(false);
  burned = $state.raw<Burned>();
  opening = $state(false);
  revealText = $state<string>();
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
  readonly reportable = $derived(this.ready !== undefined || (this.revealed?.burnable ?? false));

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
    if (this.reporting && this.reportSent) {
      return translate(this.reportSent === 'limit' ? 'open.report.limit' : 'open.report.sent');
    }
    return this.burnNote;
  });

  readonly readyChips: readonly ChipData[] = $derived.by(() => {
    const { translate } = this.#translator;
    if (!this.metadata) {
      return [];
    }
    const chips: ChipData[] = [];
    if (this.metadata.expiresAt) {
      chips.push({
        icon: 'clock',
        text: translate('open.chip.expires', { time: this.shortTime(this.metadata.expiresAt) }),
      });
    }
    chips.push({ icon: 'eye', text: this.opensLeft(this.metadata.opensRemaining) });
    if (this.metadata.passphraseRequired) {
      chips.push({ icon: 'key', text: translate('compose.passphrase.chip'), accent: true });
    }
    return chips;
  });

  readonly afterChips: readonly ChipData[] = $derived.by(() => {
    const { translate } = this.#translator;
    if (this.revealed) {
      const more = this.revealed.burnable && !this.restDeleted;
      return [
        { icon: 'check', text: translate('open.chip.now') },
        { icon: 'eye', text: more ? translate('open.chip.more') : this.opensLeft(0) },
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
    this.reporting = false;
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

  readonly closeReport = async (): Promise<void> => {
    this.reporting = false;
    await this.focus.focusAfterUpdate(() => 'reportButton');
  };

  readonly confirmDownload = async (event: MouseEvent, file: RevealedFile): Promise<void> => {
    if (event.detail > 1 || !this.confirmingDownload) {
      return;
    }
    this.#save(file);
    await this.focus.focusAfterUpdate(() => 'download');
  };

  readonly copyText = async (): Promise<void> => {
    if (!this.revealed?.text) {
      return;
    }
    try {
      await navigator.clipboard.writeText(this.revealed.text);
      this.copied.show();
    } catch {
      this.copied.hide();
    }
  };

  readonly leave = (): void => {
    this.burned = undefined;
    this.confirmingDownload = false;
    this.askingDelete = false;
    this.reporting = false;
    this.showPassphrase = false;
    this.passphrase = '';
    this.#pending = undefined;
    this.revealText = undefined;
    clearInterval(this.#revealTimer);
    this.#stopTimeline();
    this.#reader.clear();
    this.#settleShown();
  };

  readonly onConfirmKeydown = (event: KeyboardEvent): void => {
    if (event.key !== 'Escape') {
      return;
    }

    event.stopPropagation();
    void this.cancelDelete();
  };

  readonly onWindowClick = (event: MouseEvent): void => {
    if (this.reporting && !(event.target instanceof Element && event.target.closest('[data-menu]'))) {
      this.reporting = false;
    }
  };

  readonly report = async ({ reason, contact, goodFaith }: ReportSubmission): Promise<ReportOutcome> =>
    this.#reader.report({ reason, contact, goodFaith });

  readonly reportFinished = async (status: ReportStatus): Promise<void> => {
    this.reportSent = status;
    await tick();
    if (this.reporting) {
      this.focus.focus('reportPopover');
    }
  };

  readonly retry = (): void => {
    void this.#reader.load();
  };

  readonly returned = (event: PageTransitionEvent): void => {
    if (event.persisted) {
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
    this.reporting = false;
    this.askingDelete = false;
    this.burned = undefined;
    const entered = this.passphrase;
    this.passphrase = '';
    this.#revealing = true;
    void this.#revealWith(entered);
  };

  readonly toggleReport = (): void => {
    this.reporting = !this.reporting;
  };

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
        shown: async () => this.#displayed(),
        scrypt: scryptInWorker,
        now: () => Date.now(),
      },
      (view) => this.#onView(view),
    );
    onDestroy(() => {
      this.timeline.stop();
      clearInterval(this.#revealTimer);
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
    if (next.view === 'ready') {
      this.passphraseRequired = next.metadata.passphraseRequired;
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
      this.revealText = undefined;
      return;
    }
    this.#settleShown();
    if (wasRevealed) {
      return;
    }

    this.reportSent = undefined;
    this.reportRound++;
    void this.#startReveal(next.text);
  }

  async #startReveal(text: string | undefined): Promise<void> {
    clearInterval(this.#revealTimer);
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
    return new Intl.DateTimeFormat(this.#translator.language, {
      day: 'numeric',
      month: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    }).format(Date.parse(timestamp));
  }
}
