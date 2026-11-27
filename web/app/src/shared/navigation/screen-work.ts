import { createContext } from 'svelte';

export type UnsavedWork = 'draft' | 'link';

export class ScreenWork {
  #busy: () => boolean = () => false;
  #unsaved: (() => UnsavedWork | undefined) | undefined;

  get busy(): boolean {
    return this.#busy();
  }

  get unsaved(): UnsavedWork | undefined {
    return this.#unsaved?.();
  }

  watch(busy: () => boolean): () => void {
    this.#busy = busy;
    return () => {
      if (this.#busy === busy) {
        this.#busy = () => false;
      }
    };
  }

  guard(unsaved: () => UnsavedWork | undefined): () => void {
    this.#unsaved = unsaved;
    return () => {
      if (this.#unsaved === unsaved) {
        this.#unsaved = undefined;
      }
    };
  }
}

export const [getScreenWork, setScreenWork] = createContext<ScreenWork>();
