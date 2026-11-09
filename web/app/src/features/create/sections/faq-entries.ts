export const FAQ_COLUMNS = [
  [
    { key: 'faq.unopened', order: 'max-md:order-1' },
    { key: 'faq.passphrase', order: 'max-md:order-3' },
    { key: 'faq.save', order: 'max-md:order-5' },
    { key: 'faq.operator', order: 'max-md:order-7' },
  ],
  [
    { key: 'faq.opened', order: 'max-md:order-2' },
    { key: 'faq.intercepted', order: 'max-md:order-4' },
    { key: 'faq.free', order: 'max-md:order-6' },
    { key: 'faq.verify', order: 'max-md:order-8' },
  ],
] as const;

export const FAQ_KEYS = FAQ_COLUMNS.flat()
  .toSorted((first, second) => first.order.localeCompare(second.order))
  .map(({ key }) => key);
