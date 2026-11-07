export async function clearClipboard(settle: (state: 'cleared' | 'copied') => void): Promise<void> {
  try {
    await navigator.clipboard.writeText('');
    settle('cleared');
  } catch {
    settle('copied');
  }
}
