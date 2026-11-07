<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue, HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';

  import { BUTTON_BASE, BUTTON_SIZES, BUTTON_VARIANTS } from './button-classes.ts';
  import type { ButtonSize, ButtonVariant } from './button-classes.ts';

  interface Common {
    readonly variant: ButtonVariant;
    readonly size?: ButtonSize;
    readonly class?: ClassValue | undefined;
    readonly children: Snippet;
  }

  type Props = Common &
    (
      | (Omit<HTMLAnchorAttributes, 'class' | 'children'> & { readonly href: string })
      | (Omit<HTMLButtonAttributes, 'class' | 'children'> & { readonly href?: undefined })
    );

  const { variant, size = 'regular', class: className, children, ...rest }: Props = $props();
  const classes = $derived([BUTTON_VARIANTS[variant], BUTTON_BASE, BUTTON_SIZES[size], className]);
</script>

{#snippet content()}
  {#if variant === 'primary'}
    <span class="sheen" aria-hidden="true"></span>
  {/if}
  {@render children()}
{/snippet}

{#if rest.href === undefined}
  <button type="button" {...rest as HTMLButtonAttributes} class={classes}>{@render content()}</button>
{:else}
  <a {...rest as HTMLAnchorAttributes} class={classes}>{@render content()}</a>
{/if}
