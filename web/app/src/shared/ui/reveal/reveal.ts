import type { Attachment } from 'svelte/attachments';

export const REVEAL_INDEXES = [
  '[--reveal-index:0]',
  '[--reveal-index:1]',
  '[--reveal-index:2]',
  '[--reveal-index:3]',
  '[--reveal-index:4]',
  '[--reveal-index:5]',
  '[--reveal-index:6]',
  '[--reveal-index:7]',
  '[--reveal-index:8]',
  '[--reveal-index:9]',
  '[--reveal-index:10]',
  '[--reveal-index:11]',
] as const;

let observer: IntersectionObserver | undefined;
type Watcher = (
  entry: Readonly<Pick<IntersectionObserverEntry, 'isIntersecting' | 'intersectionRect' | 'boundingClientRect'>>,
) => void;
const watchers = new WeakMap<Element, Watcher>();

// The first callback measures every element in one batch, so attaching forces no layout per element.
function revealObserver(): IntersectionObserver {
  observer ??= new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        watchers.get(entry.target)?.(entry);
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
  let measured = false;
  watchers.set(element, ({ isIntersecting, intersectionRect, boundingClientRect }) => {
    if (!measured) {
      measured = true;
      if (isIntersecting) {
        observer?.unobserve(element);
      } else {
        element.dataset['reveal'] = 'pending';
      }
    } else if (intersectionRect.height >= Math.min(boundingClientRect.height * 0.25, 200)) {
      element.dataset['reveal'] = 'in';
      observer?.unobserve(element);
    }
  });
  revealObserver().observe(element);
  return () => {
    observer?.unobserve(element);
    watchers.delete(element);
    delete element.dataset['reveal'];
  };
};
