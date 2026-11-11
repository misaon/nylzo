import { onDestroy } from 'svelte';

import { saveFile } from '../../../shared/files/save-file.ts';
import type { RevealedFile } from '../reader.ts';

const READY_AFTER_MS = 1000;

type Focus = (target: 'download' | 'keepFile') => Promise<void>;

export class DownloadConfirmation {
  #timer: ReturnType<typeof setTimeout> | undefined;
  readonly #focus: Focus;
  readonly #saved: () => void;
  shown = $state(false);
  ready = $state(false);

  constructor(focus: Focus, saved: () => void) {
    this.#focus = focus;
    this.#saved = saved;
    onDestroy(() => clearTimeout(this.#timer));
  }

  readonly ask = async (event: MouseEvent): Promise<void> => {
    if (event.detail > 1) {
      return;
    }
    this.shown = true;
    this.ready = false;
    clearTimeout(this.#timer);
    this.#timer = setTimeout(() => {
      this.ready = true;
    }, READY_AFTER_MS);
    await this.#focus('keepFile');
  };

  readonly cancel = async (event: KeyboardEvent | MouseEvent): Promise<void> => {
    if (event.detail > 1) {
      return;
    }
    this.close();
    await this.#focus('download');
  };

  readonly confirm = async (event: MouseEvent, file: RevealedFile): Promise<void> => {
    if (event.detail > 1 || !this.ready) {
      return;
    }
    saveFile(file.bytes, file.name);
    this.close();
    this.#saved();
    await this.#focus('download');
  };

  readonly onKeydown = (event: KeyboardEvent): void => {
    if (event.key !== 'Escape') {
      return;
    }

    event.stopPropagation();
    void this.cancel(event);
  };

  close(): void {
    clearTimeout(this.#timer);
    this.shown = false;
    this.ready = false;
  }
}
