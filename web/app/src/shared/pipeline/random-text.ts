const ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

export function randomCharacter(): string {
  return ALPHABET[Math.floor(Math.random() * ALPHABET.length)] ?? 'A';
}

export function randomText(length: number): string {
  return Array.from({ length }, randomCharacter).join('');
}
