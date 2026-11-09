import type { Attachment } from 'svelte/attachments';

export const REVEAL_INDEXES = [
  '[--reveal-index:0]',
  '[--reveal-index:1]',
  '[--reveal-index:2]',
  '[--reveal-index:3]',
  '[--reveal-index:4]',
] as const;

let observer: IntersectionObserver | undefined;
const measured = new WeakSet<Element>();

// The first callback measures every element in one batch, so attaching forces no layout per element.
function revealObserver(): IntersectionObserver {
  observer ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        const element = entry.target;
        if (!(element instanceof HTMLElement)) {
          continue;
        }
        if (!measured.has(element)) {
          measured.add(element);
          if (entry.isIntersecting) {
            observer?.unobserve(element);
          } else {
            element.dataset['reveal'] = 'pending';
          }
        } else if (entry.intersectionRect.height >= Math.min(entry.boundingClientRect.height * 0.25, 200)) {
          element.dataset['reveal'] = 'in';
          observer?.unobserve(element);
        }
      }
    },
    { threshold: Array.from({ length: 21 }, (_, step) => step / 20) },
  );
  return observer;
}

export const reveal: Attachment<HTMLElement> = (element) => {
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
    return;
  }
  revealObserver().observe(element);
  return () => {
    observer?.unobserve(element);
    measured.delete(element);
    delete element.dataset['reveal'];
  };
};
