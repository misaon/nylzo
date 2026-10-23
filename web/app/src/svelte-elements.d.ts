import 'svelte/elements';

declare module 'svelte/elements' {
  export interface HTMLTextareaAttributes {
    autocorrect?: 'on' | 'off' | '' | undefined | null;
  }
}
