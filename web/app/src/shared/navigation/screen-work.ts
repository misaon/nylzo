import { createContext } from 'svelte';

export class ScreenWork {
  #busy: () => boolean = () => false;

  get busy(): boolean {
    return this.#busy();
  }

  watch(busy: () => boolean): () => void {
    this.#busy = busy;
    return () => {
      if (this.#busy === busy) {
        this.#busy = () => false;
      }
    };
  }
}

export const [getScreenWork, setScreenWork] = createContext<ScreenWork>();
