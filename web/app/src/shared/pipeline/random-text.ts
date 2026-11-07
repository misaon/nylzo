const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

// 256 is a multiple of the 64 letters, so a random byte picks each letter equally often.
function letterOf(byte: number): string {
  return ALPHABET[byte % ALPHABET.length] ?? 'A';
}

export function randomCharacter(): string {
  return randomText(1);
}

export function randomText(length: number): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(length)), letterOf).join('');
}
