import type { Attachment } from 'svelte/attachments';

let observer: IntersectionObserver | undefined;

function revealObserver(): IntersectionObserver {
  observer ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        const element = entry.target;
        if (
          element instanceof HTMLElement &&
          entry.intersectionRect.height >= Math.min(entry.boundingClientRect.height * 0.25, 200)
        ) {
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
  const box = element.getBoundingClientRect();
  if (box.top < innerHeight && box.bottom > 0) {
    return;
  }
  element.dataset['reveal'] = 'pending';
  revealObserver().observe(element);
  return () => {
    observer?.unobserve(element);
    delete element.dataset['reveal'];
  };
};
