import {
  formatLink,
  generatePassphrase,
  ID_LENGTH,
  KDF_NONE,
  KDF_SCRYPT,
  loadPassphraseWords,
  MAX_FILE_BYTES,
  MAX_TEXT_BYTES,
  MIN_PASSPHRASE_CHARACTERS,
  OBJECT_HASH_LENGTH,
  SECRET_LENGTH,
} from '@nylzo/core';
import { flushSync, tick } from 'svelte';
import type { Attachment } from 'svelte/attachments';
import { prefersReducedMotion } from 'svelte/motion';
import { SvelteDate } from 'svelte/reactivity';

import { createApi } from '../../../shared/api/api.ts';
import type { InstanceLimits } from '../../../shared/api/responses.ts';
import { solvePowInWorker, stopRunningWorkers, stretchInWorker } from '../../../shared/kdf/kdf.ts';
import { formatBytes, formatDuration, formatWait } from '../../../shared/language/format.ts';
import type { Translator } from '../../../shared/language/translator.svelte.ts';
import { instanceOrigin, servedByLocalClient } from '../../../shared/local-client/instance-origin.ts';
import type { UnsavedWork } from '../../../shared/navigation/screen-work.ts';
import { PacedTimeline } from '../../../shared/pipeline/paced-timeline.svelte.ts';
import type { TimelineSettings } from '../../../shared/pipeline/paced-timeline.svelte.ts';
import type { PipelineNode } from '../../../shared/pipeline/pipeline.ts';
import { randomText } from '../../../shared/pipeline/random-text.ts';
import { browserSentStore, rememberSent } from '../../../shared/sent-records/sent-store.ts';
import type { SentStore } from '../../../shared/sent-records/sent-store.ts';
import { ttlChoices, ttlKey } from '../../../shared/ttl/ttl-choices.ts';
import { FocusTargets } from '../../../shared/ui/focus/focus.ts';
import { Glide } from '../../../shared/ui/transition/glide.ts';
import {
  CreateError,
  createMessage,
  passphraseIsWeak,
  validateCreate,
  WEAK_BELOW_CHARACTERS,
  WEAK_BELOW_WORDS,
} from '../create-message.ts';
import type { Created, CreateField, CreateInput, CreateProblem, CreateProgress } from '../create-message.ts';
import { failureKey, problemKey, readAttachment } from './create-form.ts';
import type { SendFailure } from './create-form.ts';

export type Menu = 'opens' | 'security' | 'ttl';

type ChosenAttachment = Awaited<ReturnType<typeof readAttachment>>;

export type ComposerTarget = 'file' | 'opens' | 'passphrase' | 'security' | 'submit' | 'text' | 'ttl';

export type Remembered = 'failed' | 'off' | 'remembered' | 'saving';

interface SealedMessage {
  readonly created: Created;
  readonly opensMax: number;
  readonly passphrase: string;
}

interface Composing {
  readonly phase: 'compose';
}

interface Encrypting {
  readonly phase: 'encrypting';
  readonly withPassphrase: boolean;
  readonly pending?: SealedMessage;
}

interface Done {
  readonly phase: 'done';
  readonly sealed: SealedMessage;
}

type ComposerState = Composing | Done | Encrypting;

function characterCount(text: string): number {
  return text.normalize('NFC').match(/./gsu)?.length ?? 0;
}

const REACHED: Readonly<Record<CreateProgress['step'], number>> = {
  preparing: 0,
  deriving: 1,
  encrypting: 2,
  proving: 3,
  uploading: 3,
};
const STEPS = 5;
const MIN_MS = 1400;
const REDUCED_MIN_MS = 600;
const SAVE_TIMEOUT_MS = 5000;
const DEFAULT_TTL_SECONDS = 86_400;
const UNEXPECTED_FAILURE: SendFailure = { kind: 'failed' };
const CIPHER_SOURCE_CHARACTERS = 140;
const CIPHER_LEAD = 1.3;
const CIPHER_GROWTH = 1100;

function timelineSettings(): TimelineSettings {
  return { minMs: prefersReducedMotion.current ? REDUCED_MIN_MS : MIN_MS, steps: STEPS };
}

function cipherText(plain: string, progress: number): string {
  const cut = Math.floor(plain.length * Math.min(1, progress * CIPHER_LEAD));
  return randomText(cut) + plain.slice(cut) + randomText(Math.floor(progress * CIPHER_GROWTH));
}

export class Composer {
  #state = $state.raw<ComposerState>({ phase: 'compose' });
  #run = 0;
  #suggestion = 0;
  #submitting = false;
  #sentStore: ReturnType<typeof browserSentStore> | undefined;
  readonly #sessionStore: SentStore;
  readonly #api = createApi();
  readonly #encoder = new TextEncoder();
  readonly #translator: Translator;

  limits = $state.raw<InstanceLimits>();
  loadFailed = $state(false);
  text = $state('');
  file = $state.raw<File>();
  dragging = $state(false);
  ttlSeconds = $state(DEFAULT_TTL_SECONDS);
  opensMax = $state(1);
  protect = $state(false);
  passphrase = $state('');
  canKeep = $state(true);
  canTrackInTab = $state(true);
  menu = $state<Menu>();
  problems = $state.raw<readonly CreateProblem[]>([]);
  failure = $state.raw<SendFailure>();
  proving = $state(false);
  cipher = $state('');
  left = $state(false);
  remembered = $state<Remembered>('off');
  kept = $state(false);
  linkCopied = $state(false);
  trackingPanelLeft = $state(false);
  readonly focus = new FocusTargets<ComposerTarget>();
  readonly glide = new Glide();

  readonly timeline = new PacedTimeline(timelineSettings);

  readonly phase = $derived(this.#state.phase);
  readonly sealed = $derived(this.#state.phase === 'done' ? this.#state.sealed : undefined);
  readonly sealingWithPassphrase = $derived(this.#state.phase === 'encrypting' && this.#state.withPassphrase);
  readonly busy = $derived(this.phase !== 'compose');

  readonly unsaved = $derived.by((): UnsavedWork | undefined => {
    const state = this.#state;
    const linkCreated = state.phase === 'done' || (state.phase === 'encrypting' && state.pending !== undefined);
    if (!linkCreated) {
      return this.text.trim() !== '' || this.file !== undefined ? 'draft' : undefined;
    }
    const tracked = this.remembered === 'saving' || this.remembered === 'remembered';
    return this.linkCopied || tracked ? undefined : 'link';
  });

  // A link of the final length lets the created panel take its size at the click, so nothing moves when it arrives.
  readonly placeholder = $derived(
    this.phase === 'encrypting'
      ? {
          link: formatLink(instanceOrigin(), {
            kdfId: this.sealingWithPassphrase ? KDF_SCRYPT : KDF_NONE,
            id: new Uint8Array(ID_LENGTH),
            secret: new Uint8Array(SECRET_LENGTH),
            objectHash: new Uint8Array(OBJECT_HASH_LENGTH),
          }),
          expiresAt: new SvelteDate(Date.now() + this.ttlSeconds * 1000).toISOString(),
        }
      : undefined,
  );

  readonly usesPassphrase = $derived.by(() => {
    const state = this.#state;
    switch (state.phase) {
      case 'compose': {
        return this.protect && this.passphrase !== '';
      }
      case 'encrypting': {
        return state.withPassphrase;
      }
      case 'done': {
        return state.sealed.passphrase !== '';
      }
    }
  });

  readonly textBytes = $derived(this.#encoder.encode(this.text).length);
  readonly textLimit = $derived(Math.min(this.limits?.maxTextBytes ?? MAX_TEXT_BYTES, MAX_TEXT_BYTES));
  readonly fileLimit = $derived(this.limits?.maxFileBytes ?? MAX_FILE_BYTES);
  readonly ttlOptions = $derived(this.limits ? ttlChoices(this.limits) : [this.ttlSeconds]);
  readonly maxOpens = $derived(this.limits?.maxOpens ?? 1);
  readonly missingCharacters = $derived(Math.max(1, MIN_PASSPHRASE_CHARACTERS - characterCount(this.passphrase)));
  readonly weak = $derived(this.protect && this.passphrase !== '' && passphraseIsWeak(this.passphrase));

  readonly nodes: readonly PipelineNode[] = $derived.by(() => {
    const { translate } = this.#translator;
    return [
      {
        id: 'key',
        label: translate('step.key.label'),
        icon: 'key',
        title: translate('step.key.title'),
        detail: translate('step.key.detail'),
      },
      {
        id: 'derive',
        label: translate('step.derive.label'),
        icon: 'fork',
        title: translate('step.derive.title'),
        detail: translate(this.usesPassphrase ? 'step.derive.detail.passphrase' : 'step.derive.detail'),
      },
      {
        id: 'encrypt',
        label: translate('step.encrypt.label'),
        icon: 'lock',
        title: translate('step.encrypt.title'),
        detail: translate('step.encrypt.detail'),
      },
      {
        id: 'send',
        label: translate('step.send.label'),
        icon: 'upload',
        title: translate(this.proving ? 'step.send.title.pow' : 'step.send.title'),
        detail: translate(this.proving ? 'step.send.detail.pow' : 'step.send.detail'),
      },
      {
        id: 'link',
        label: translate('step.link.label'),
        icon: 'link',
        title: translate('step.link.title'),
        detail: translate('step.link.detail'),
      },
    ];
  });

  readonly problemOf = $derived.by(() => {
    const { translate, plural, language } = this.#translator;
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of this.problems) {
      messages[problem.field] ??= translate(problemKey(problem), {
        size: formatBytes(problem.field === 'text' ? this.textLimit : this.fileLimit, language),
        max: this.maxOpens,
        min: MIN_PASSPHRASE_CHARACTERS,
        missing: plural('count.character', this.missingCharacters),
      });
    }
    return messages;
  });

  readonly passphraseMessage = $derived.by(() => {
    if (this.problemOf.passphrase !== undefined) {
      return this.problemOf.passphrase;
    }
    const { translate } = this.#translator;
    return this.weak
      ? translate('create.passphrase.weak', { words: WEAK_BELOW_WORDS, characters: WEAK_BELOW_CHARACTERS })
      : '';
  });

  readonly failureNotice = $derived.by(() => {
    if (this.loadFailed) {
      return this.#translator.translate('failure.instance');
    }
    return this.failure ? this.#failureText(this.failure) : '';
  });

  readonly problemNotice = $derived.by(() => {
    const { content, text, file, ttl, opens, passphrase } = this.problemOf;
    return content ?? text ?? file ?? ttl ?? opens ?? (this.menu === 'security' ? undefined : passphrase) ?? '';
  });

  readonly notice = $derived(this.failureNotice || this.problemNotice);

  // A browser save that timed out may still land and be removed later, so only the tab can take a second try.
  readonly #keepsNext = $derived(this.canKeep && this.remembered !== 'failed');
  readonly trackable = $derived(this.#keepsNext || this.canTrackInTab);
  readonly keeps = $derived(
    this.remembered === 'saving' || this.remembered === 'remembered' ? this.kept : this.#keepsNext,
  );

  readonly stepTitle = $derived(
    this.nodes.find((_node, index) => this.phase === 'encrypting' && index === this.timeline.paced.step)?.title ?? '',
  );

  readonly start: Attachment = () => {
    if (servedByLocalClient()) {
      this.canKeep = false;
    }
    // Without the Navigation API every move between pages loads a new document, which would forget a tab-only record.
    if (typeof navigation === 'undefined') {
      this.canTrackInTab = false;
    }
    void this.loadLimits();
    return () => this.#discard();
  };

  readonly remember = async (label: string): Promise<void> => {
    const sealed = this.sealed;
    const keep = this.#keepsNext;
    if (!sealed || !this.trackable || this.remembered === 'saving' || this.remembered === 'remembered') {
      return;
    }
    this.remembered = 'saving';
    this.kept = keep;
    const stored = await this.#rememberMessage(sealed.created, label, keep);
    if (this.sealed === sealed) {
      this.remembered = stored ? 'remembered' : 'failed';
    }
  };

  readonly another = async (): Promise<void> => {
    this.#reset();
    await this.focus.focusAfterUpdate('text');
  };

  readonly closeMenu = (trigger: Menu): void => {
    this.menu = undefined;
    this.focus.focus(trigger);
  };

  readonly guardLeaving = (event: Event): void => {
    if (this.unsaved) {
      event.preventDefault();
      return;
    }
    this.forgetPassphrase();
  };

  readonly forgetPassphrase = (): void => {
    this.passphrase = '';
    flushSync();
  };

  readonly leave = (): void => {
    this.#discard();
    // Commit removal before pagehide returns and the browser freezes this document.
    flushSync();
  };

  readonly loadLimits = async (): Promise<void> => {
    this.loadFailed = false;
    try {
      const instance = await this.#api.instance();
      this.limits = instance.limits;
      this.ttlSeconds = instance.limits.defaultTtlSeconds;
      this.opensMax = Math.min(this.opensMax, instance.limits.maxOpens);
    } catch {
      this.loadFailed = true;
    }
  };

  readonly onDragLeave = (event: DragEvent): void => {
    const { relatedTarget, currentTarget } = event;
    const stillInside =
      relatedTarget instanceof Node && currentTarget instanceof Node && currentTarget.contains(relatedTarget);
    if (!stillInside) {
      this.dragging = false;
    }
  };

  readonly onDragOver = (event: DragEvent): void => {
    if (this.busy || !event.dataTransfer?.types.includes('Files')) {
      return;
    }
    event.preventDefault();
    this.dragging = true;
  };

  readonly onDrop = (event: DragEvent): void => {
    if (!event.dataTransfer?.types.includes('Files')) {
      return;
    }
    event.preventDefault();
    this.dragging = false;
    if (this.busy) {
      return;
    }
    this.pickFile(event.dataTransfer.files[0]);
    if (event.dataTransfer.items[0]?.webkitGetAsEntry()?.isDirectory === true) {
      this.problems = [...this.problems, { field: 'file', code: 'unreadable' }];
    }
  };

  readonly onWindowClick = (event: MouseEvent): void => {
    if (this.menu && !(event.target instanceof Element && event.target.closest('[data-menu]'))) {
      this.menu = undefined;
    }
  };

  readonly pickFile = (next?: File): void => {
    this.file = next;
    this.#forgetProblems('file', 'content');
    if (next && this.limits && next.size > this.limits.maxFileBytes) {
      this.problems = [...this.problems, { field: 'file', code: 'too_large' }];
    }
  };

  readonly removeFile = (): void => {
    this.pickFile();
    const input = this.focus.get('file');
    if (input instanceof HTMLInputElement) {
      input.value = '';
    }
  };

  readonly returned = (event: PageTransitionEvent): void => {
    if (event.persisted) {
      location.reload();
    }
  };

  readonly submit = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    if (!this.limits || this.busy || this.#submitting) {
      return;
    }
    this.#submitting = true;
    try {
      await this.#send(this.limits);
    } finally {
      this.#submitting = false;
    }
  };

  readonly toggleMenu = (next: Menu): void => {
    this.menu = this.menu === next ? undefined : next;
    if (this.menu === 'security') {
      loadPassphraseWords(this.#translator.language).catch(() => null);
    }
  };

  // The word list loads on demand, so what the sender typed meanwhile must win.
  readonly generatePassphrase = async (): Promise<void> => {
    const suggestion = ++this.#suggestion;
    const before = this.passphrase;
    const passphrase = await generatePassphrase(this.#translator.language).catch(() => null);
    if (
      passphrase !== null &&
      suggestion === this.#suggestion &&
      this.protect &&
      !this.busy &&
      this.passphrase === before
    ) {
      this.passphrase = passphrase;
      this.#forgetProblems('passphrase');
    }
  };

  // Words that arrive after the sender reached the field would join the characters they type next.
  readonly reachPassphrase = (): void => {
    this.#suggestion++;
  };

  readonly toggleProtect = async (): Promise<void> => {
    this.protect = !this.protect;
    if (this.protect) {
      if (this.passphrase === '') {
        await this.generatePassphrase();
      }
      await this.focus.focusAfterUpdate('passphrase');
      return;
    }
    this.passphrase = '';
    this.#forgetProblems('passphrase');
  };

  constructor(translator: Translator, sessionStore: SentStore) {
    this.#translator = translator;
    this.#sessionStore = sessionStore;
  }

  #failureText(failure: SendFailure): string {
    const { translate, plural } = this.#translator;
    const retryAfter =
      failure.kind === 'rate_limited' || failure.kind === 'unavailable' ? failure.retryAfter : undefined;
    return translate(failureKey(failure), { time: formatWait(retryAfter, plural) });
  }

  #finish(result: SealedMessage): void {
    this.timeline.stop();
    this.#state = { phase: 'done', sealed: result };
    this.#forgetTracking();
  }

  #forgetTracking(): void {
    this.remembered = 'off';
    this.kept = false;
    this.linkCopied = false;
    this.trackingPanelLeft = false;
  }

  #forgetProblems(...fields: readonly CreateField[]): void {
    this.problems = this.problems.filter((problem) => !fields.includes(problem.field));
  }

  #onProgress(progress: CreateProgress): void {
    this.proving = progress.step === 'proving';
    this.timeline.reached = Math.max(this.timeline.reached, REACHED[progress.step]);
    this.timeline.fraction = 'fraction' in progress ? progress.fraction : undefined;
  }

  #discard(): void {
    this.#sentStore?.close();
    this.#sentStore = undefined;
    stopRunningWorkers();
    this.left = true;
    this.#run++;
    this.timeline.stop();
    this.cipher = '';
    this.menu = undefined;
    this.#reset();
  }

  async #rememberMessage(created: Created, label: string, keep: boolean): Promise<boolean> {
    const expiresAt = Date.parse(created.expiresAt);
    const record = {
      id: created.id,
      createdAt: Math.min(Date.now(), expiresAt),
      expiresAt,
      label: label.trim(),
      manageKey: created.manageKey,
    };
    if (!keep) {
      return rememberSent(this.#sessionStore, record, SAVE_TIMEOUT_MS);
    }
    this.#sentStore ??= browserSentStore();
    return rememberSent(this.#sentStore, record, SAVE_TIMEOUT_MS);
  }

  #reset(): void {
    this.#forgetTracking();
    this.text = '';
    this.passphrase = '';
    this.protect = false;
    this.removeFile();
    this.#enter({ phase: 'compose' });
  }

  async #send(limits: InstanceLimits): Promise<void> {
    const run = this.#run;
    const stale = () => run !== this.#run;
    this.menu = undefined;
    this.failure = undefined;
    const chosen = await readAttachment(this.file, limits);
    try {
      await this.#sendChosen(chosen, limits, stale);
    } finally {
      if ('file' in chosen) {
        chosen.file?.bytes.fill(0);
      }
    }
  }

  async #sendChosen(chosen: ChosenAttachment, limits: InstanceLimits, stale: () => boolean): Promise<void> {
    if (stale()) {
      return;
    }
    if ('problem' in chosen) {
      await this.#showProblems([chosen.problem]);
      return;
    }
    const input: CreateInput = {
      text: this.text,
      file: chosen.file,
      ttlSeconds: this.ttlSeconds,
      opensMax: this.opensMax,
      passphrase: this.protect ? this.passphrase : '',
    };
    const unset: CreateProblem[] =
      this.protect && this.passphrase === '' ? [{ field: 'passphrase', code: 'too_short' }] : [];
    const invalid = [...unset, ...validateCreate(input, limits)];
    if (invalid.length > 0) {
      await this.#showProblems(invalid);
      return;
    }
    this.problems = [];
    const withPassphrase = input.passphrase !== '';
    this.#startTimeline(this.text.trim() === '' ? (this.file?.name ?? '') : this.text, withPassphrase);
    try {
      const created = await createMessage(
        input,
        { api: this.#api, origin: instanceOrigin(), stretch: stretchInWorker, solvePow: solvePowInWorker },
        (progress) => {
          if (!stale()) {
            this.#onProgress(progress);
          }
        },
      );
      if (stale()) {
        return;
      }
      this.text = '';
      this.passphrase = '';
      this.removeFile();
      this.#state = {
        phase: 'encrypting',
        withPassphrase,
        pending: { created, opensMax: this.opensMax, passphrase: input.passphrase },
      };
      this.timeline.complete();
    } catch (error) {
      if (stale()) {
        return;
      }
      this.timeline.stop();
      this.#enter({ phase: 'compose' });
      const failure = error instanceof CreateError ? error.failure : UNEXPECTED_FAILURE;
      if (failure.kind === 'invalid') {
        await this.#showProblems(failure.problems);
        return;
      }
      this.failure = failure;
      await this.focus.focusAfterUpdate('submit');
    }
  }

  async #showProblems(problems: readonly CreateProblem[]): Promise<void> {
    const passphraseProblem = problems.some((problem) => problem.field === 'passphrase');
    this.problems = problems;
    if (passphraseProblem) {
      this.menu = 'security';
    }
    await tick();
    if (passphraseProblem) {
      await this.focus.focusAfterUpdate('passphrase');
    } else if (problems.some((problem) => problem.field === 'content' || problem.field === 'text')) {
      this.focus.focus('text');
    } else if (problems.some((problem) => problem.field === 'file')) {
      this.focus.focus('file');
    }
  }

  // The intro collapses or returns with these phases, so what follows glides into place.
  #enter(state: Composing | Encrypting): void {
    void this.glide.around(() => {
      this.#state = state;
    });
  }

  #startTimeline(source: string, withPassphrase: boolean): void {
    const plain = source.replaceAll(/\s+/gu, ' ').slice(0, CIPHER_SOURCE_CHARACTERS);
    this.proving = false;
    this.cipher = cipherText(plain, 0);
    this.#enter({ phase: 'encrypting', withPassphrase });
    let cipherStep = -1;
    this.timeline.start(() => {
      const { step, progress, finished } = this.timeline.paced;
      if (!prefersReducedMotion.current || step !== cipherStep) {
        cipherStep = step;
        this.cipher = cipherText(plain, progress);
      }
      const state = this.#state;
      if (finished && state.phase === 'encrypting' && state.pending) {
        this.#finish(state.pending);
      }
    });
  }

  opensLabel(count: number): string {
    return this.#translator.plural('compose.opens', count);
  }

  ttlLabel(seconds: number): string {
    const key = ttlKey(seconds);
    return key ? this.#translator.translate(key) : formatDuration(seconds, this.#translator.language);
  }
}
