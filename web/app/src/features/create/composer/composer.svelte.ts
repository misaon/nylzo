import type { Attachment } from 'svelte/attachments';

import { generatePassphrase, MAX_FILE_BYTES, MAX_TEXT_BYTES, MIN_PASSPHRASE_CHARACTERS } from '@nylzo/core';
import { flushSync, tick } from 'svelte';
import { prefersReducedMotion } from 'svelte/motion';

import type { InstanceLimits } from '../../../shared/api/responses.ts';
import type { Translator } from '../../../shared/language/translator.svelte.ts';
import type { PipelineNode } from '../../../shared/pipeline/pipeline.ts';

import { createApi } from '../../../shared/api/api.ts';
import { scryptInWorker, solvePowInWorker } from '../../../shared/kdf/kdf.ts';
import { formatBytes, formatDuration, formatWait } from '../../../shared/language/format.ts';
import { instanceOrigin, servedByLocalClient } from '../../../shared/local-client/instance-origin.ts';
import { PacedTimeline, type TimelineSettings } from '../../../shared/pipeline/paced-timeline.svelte.ts';
import { randomCharacter, randomText } from '../../../shared/pipeline/random-text.ts';
import { browserSentStore, rememberSent, type SentStore } from '../../../shared/sent-records/sent-store.ts';
import { FocusTargets } from '../../../shared/ui/focus.ts';
import {
  type Created,
  CreateError,
  type CreateFailure,
  type CreateField,
  type CreateInput,
  createMessage,
  type CreateProblem,
  type CreateProgress,
  passphraseIsWeak,
  validateCreate,
  WEAK_BELOW_CHARACTERS,
  WEAK_BELOW_WORDS,
} from '../create-message.ts';
import { failureKey, problemKey, readAttachment, ttlChoices, ttlKey } from './create-form.ts';

export type Menu = 'opens' | 'security' | 'ttl';

type ChosenAttachment = Awaited<ReturnType<typeof readAttachment>>;

export type ComposerTarget = 'file' | 'opens' | 'passphrase' | 'security' | 'submit' | 'text' | 'ttl';

type Phase = 'compose' | 'done' | 'encrypting';

type Remembered = 'failed' | 'off' | 'remembered' | 'saving';

interface SealedMessage {
  readonly created: Created;
  readonly opensMax: number;
  readonly passphrase: boolean;
  readonly saved?: Promise<boolean> | undefined;
}

const REACHED: Readonly<Record<CreateProgress['step'], number>> = {
  preparing: 0,
  deriving: 1,
  encrypting: 2,
  proving: 3,
  uploading: 3,
};
const STEPS = 5;
const MIN_MS = 5000;
const REDUCED_MIN_MS = 1200;
const SAVE_TIMEOUT_MS = 5000;
const DEFAULT_TTL_SECONDS = 86_400;
const CIPHER_SOURCE_CHARACTERS = 140;
const CIPHER_LEAD = 1.3;
const CIPHER_GROWTH = 1100;

function timelineSettings(): TimelineSettings {
  return { minMs: prefersReducedMotion.current ? REDUCED_MIN_MS : MIN_MS, steps: STEPS };
}

function cipherText(plain: string, progress: number): string {
  const cut = Math.floor(plain.length * Math.min(1, progress * CIPHER_LEAD));
  const scrambled = Array.from({ length: plain.length }, (_, index) =>
    index < cut ? randomCharacter() : plain.charAt(index),
  );
  return scrambled.join('') + randomText(Math.floor(progress * CIPHER_GROWTH));
}

export class Composer {
  #pending: SealedMessage | undefined;
  #run = 0;
  #submitting = false;
  #sentStore: SentStore | undefined;
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
  passphraseVisible = $state(false);
  remember = $state(false);
  canRemember = $state(true);
  menu = $state<Menu>();
  problems = $state.raw<readonly CreateProblem[]>([]);
  failure = $state.raw<CreateFailure>();
  phase = $state<Phase>('compose');
  proving = $state(false);
  cipher = $state('');
  sealed = $state.raw<SealedMessage>();
  sealingWithPassphrase = $state(false);
  remembered = $state<Remembered>('off');
  readonly focus = new FocusTargets<ComposerTarget>();

  readonly timeline = new PacedTimeline(timelineSettings);

  readonly busy = $derived(this.phase !== 'compose');

  readonly usesPassphrase = $derived(
    this.phase === 'compose' ? this.protect && this.passphrase !== '' : this.sealingWithPassphrase,
  );

  readonly textBytes = $derived(this.#encoder.encode(this.text).length);
  readonly textLimit = $derived(Math.min(this.limits?.maxTextBytes ?? MAX_TEXT_BYTES, MAX_TEXT_BYTES));
  readonly fileLimit = $derived(this.limits?.maxFileBytes ?? MAX_FILE_BYTES);
  readonly ttlOptions = $derived(this.limits ? ttlChoices(this.limits) : [this.ttlSeconds]);
  readonly maxOpens = $derived(this.limits?.maxOpens ?? 1);
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
    const { translate, language } = this.#translator;
    const messages: Partial<Record<CreateField, string>> = {};
    for (const problem of this.problems) {
      messages[problem.field] ??= translate(problemKey(problem), {
        size: formatBytes(problem.field === 'text' ? this.textLimit : this.fileLimit, language),
        max: this.maxOpens,
        min: MIN_PASSPHRASE_CHARACTERS,
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

  readonly notice = $derived.by(() => {
    const { translate } = this.#translator;
    if (this.loadFailed) {
      return translate('failure.instance');
    }
    if (this.failure) {
      return this.#failureText(this.failure);
    }
    const { content, text, file, ttl, opens, passphrase } = this.problemOf;
    return content ?? text ?? file ?? ttl ?? opens ?? passphrase ?? '';
  });

  readonly stepTitle = $derived(this.phase === 'encrypting' ? (this.nodes[this.timeline.paced.step]?.title ?? '') : '');

  readonly start: Attachment = () => {
    if (servedByLocalClient()) {
      this.canRemember = false;
      this.remember = false;
    }
    void this.loadLimits();
    return () => this.timeline.stop();
  };

  readonly another = async (): Promise<void> => {
    this.#reset();
    await this.focus.focusAfterUpdate(() => 'text');
  };

  readonly closeMenu = (trigger: Menu): void => {
    this.menu = undefined;
    this.focus.focus(trigger);
  };

  readonly forgetPassphrase = (): void => {
    this.passphrase = '';
    this.passphraseVisible = false;
    flushSync();
  };

  readonly leave = (): void => {
    this.#run++;
    this.timeline.stop();
    this.#pending = undefined;
    this.cipher = '';
    this.menu = undefined;
    this.#reset();
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
    this.problems = this.problems.filter((problem) => problem.field !== 'file' && problem.field !== 'content');
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
  };

  readonly generatePassphrase = (): void => {
    this.passphrase = generatePassphrase();
    this.passphraseVisible = true;
    this.problems = this.problems.filter((problem) => problem.field !== 'passphrase');
  };

  readonly toggleProtect = async (): Promise<void> => {
    this.protect = !this.protect;
    if (this.protect) {
      if (this.passphrase === '') {
        this.generatePassphrase();
      }
      await this.focus.focusAfterUpdate(() => 'passphrase');
      return;
    }
    this.passphrase = '';
    this.passphraseVisible = false;
    this.problems = this.problems.filter((problem) => problem.field !== 'passphrase');
  };

  constructor(translator: Translator) {
    this.#translator = translator;
  }

  #failureText(failure: CreateFailure): string {
    const { translate, language } = this.#translator;
    const retryAfter =
      failure.kind === 'rate_limited' || failure.kind === 'unavailable' ? failure.retryAfter : undefined;
    return translate(failureKey(failure), { time: formatWait(retryAfter, language) });
  }

  #finish(result: SealedMessage): void {
    this.timeline.stop();
    this.#pending = undefined;
    this.sealed = result;
    this.phase = 'done';
    this.remembered = result.saved ? 'saving' : 'off';
    if (result.saved) {
      void this.#showRemembered(result, result.saved);
    }
  }

  #onProgress(progress: CreateProgress): void {
    this.proving = progress.step === 'proving';
    this.timeline.reached = Math.max(this.timeline.reached, REACHED[progress.step]);
    this.timeline.fraction = 'fraction' in progress ? progress.fraction : undefined;
  }

  async #rememberMessage(created: Created): Promise<boolean> {
    const expiresAt = Date.parse(created.expiresAt);
    const record = {
      id: created.id,
      createdAt: Math.min(Date.now(), expiresAt),
      expiresAt,
      manageKey: created.manageKey,
    };
    this.#sentStore ??= browserSentStore();
    return rememberSent(this.#sentStore, record, SAVE_TIMEOUT_MS);
  }

  #reset(): void {
    this.sealed = undefined;
    this.remembered = 'off';
    this.text = '';
    this.passphrase = '';
    this.passphraseVisible = false;
    this.protect = false;
    this.removeFile();
    this.phase = 'compose';
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
    this.sealingWithPassphrase = input.passphrase !== '';
    this.#startTimeline(this.text.trim() === '' ? (this.file?.name ?? '') : this.text);
    try {
      const created = await createMessage(
        input,
        { api: this.#api, origin: instanceOrigin(), scrypt: scryptInWorker, solvePow: solvePowInWorker },
        (progress) => {
          if (!stale()) {
            this.#onProgress(progress);
          }
        },
      );
      if (stale()) {
        return;
      }
      const saved = this.remember && this.canRemember ? this.#rememberMessage(created) : undefined;
      this.text = '';
      this.passphrase = '';
      this.passphraseVisible = false;
      this.passphraseVisible = false;
      this.removeFile();
      this.#pending = { created, opensMax: this.opensMax, passphrase: input.passphrase !== '', saved };
      this.timeline.complete();
    } catch (error) {
      if (stale()) {
        return;
      }
      this.timeline.stop();
      this.phase = 'compose';
      if (error instanceof CreateError && error.failure.kind === 'invalid') {
        await this.#showProblems(error.failure.problems);
        return;
      }
      this.failure = error instanceof CreateError ? error.failure : { kind: 'failed' };
      await this.focus.focusAfterUpdate(() => 'submit');
    }
  }

  async #showProblems(problems: readonly CreateProblem[]): Promise<void> {
    this.problems = problems;
    await tick();
    if (problems.some((problem) => problem.field === 'passphrase')) {
      this.menu = 'security';
      await this.focus.focusAfterUpdate(() => 'passphrase');
    } else if (problems.some((problem) => problem.field === 'content' || problem.field === 'text')) {
      this.focus.focus('text');
    } else if (problems.some((problem) => problem.field === 'file')) {
      this.focus.focus('file');
    }
  }

  async #showRemembered(result: SealedMessage, saved: Promise<boolean>): Promise<void> {
    const stored = await saved;
    if (this.sealed === result) {
      this.remembered = stored ? 'remembered' : 'failed';
    }
  }

  #startTimeline(source: string): void {
    const plain = source.replaceAll(/\s+/g, ' ').slice(0, CIPHER_SOURCE_CHARACTERS);
    this.proving = false;
    this.cipher = cipherText(plain, 0);
    this.phase = 'encrypting';
    this.timeline.start(() => {
      this.cipher = cipherText(plain, this.timeline.paced.progress);
      if (this.timeline.paced.finished && this.#pending) {
        this.#finish(this.#pending);
      }
    });
  }

  opensLabel(count: number): string {
    return this.#translator.translate(count === 1 ? 'compose.opens.one' : 'compose.opens.other', { count });
  }

  ttlLabel(seconds: number): string {
    const key = ttlKey(seconds);
    return key ? this.#translator.translate(key) : formatDuration(seconds, this.#translator.language);
  }
}
