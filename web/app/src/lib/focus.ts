export function keepFocus(fallback: HTMLElement | null | undefined): void {
  const active = document.activeElement;
  const lost = !active || active === document.body || !active.isConnected || (active instanceof HTMLButtonElement && active.disabled);
  if (lost) {
    fallback?.focus();
  }
}
