export type ButtonVariant = 'primary' | 'tint' | 'ghost' | 'danger';
export type ButtonSize = 'tight' | 'compact' | 'field' | 'regular' | 'large';

export const BUTTON_VARIANTS: Readonly<Record<ButtonVariant, string>> = {
  primary: 'button-primary hover-lift',
  tint: 'button-tint',
  ghost: 'button-ghost',
  danger: 'button-danger',
};

export const BUTTON_BASE =
  'group/action inline-flex shrink-0 items-center justify-center font-medium whitespace-nowrap';

export const BUTTON_SIZES: Readonly<Record<ButtonSize, string>> = {
  tight: 'h-11 gap-1.5 rounded-control px-3 text-sm md:h-10',
  compact: 'h-11 gap-2 rounded-control px-3 text-[13px] md:h-9',
  field: 'h-11 gap-2 rounded-control px-4 text-sm md:h-9',
  regular: 'h-11 gap-2 rounded-control px-4 text-sm md:h-10',
  large: 'h-12 gap-2.5 rounded-pane px-6 text-[15px]',
};
