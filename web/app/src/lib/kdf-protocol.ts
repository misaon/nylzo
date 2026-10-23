export type KdfRequest =
  | { readonly kind: 'scrypt'; readonly passphrase: string; readonly saltPw: Uint8Array<ArrayBuffer> }
  | {
      readonly kind: 'pow';
      readonly challenge: string;
      readonly id: Uint8Array<ArrayBuffer>;
      readonly difficulty: number;
      readonly deadline: number;
    };

export type KdfResponse =
  | { readonly kind: 'progress'; readonly fraction: number }
  | { readonly kind: 'key'; readonly key: Uint8Array<ArrayBuffer> }
  | { readonly kind: 'token'; readonly token: string }
  | { readonly kind: 'error'; readonly timeout: boolean };
