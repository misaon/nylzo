import type { Attachment } from 'svelte/attachments';

let observer: IntersectionObserver | undefined;

// The margin marks a section as on screen before any of its reveals can start, so no visible animation pauses.
function offscreenObserver(): IntersectionObserver {
  observer ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        entry.target.toggleAttribute('data-paused', !entry.isIntersecting);
      }
    },
    { rootMargin: '25% 0px' },
  );
  return observer;
}

export const pauseOffscreen: Attachment<HTMLElement> = (element) => {
  offscreenObserver().observe(element);
  return () => {
    observer?.unobserve(element);
    delete element.dataset['paused'];
  };
};
