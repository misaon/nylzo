<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue } from 'svelte/elements';

  import {
    CHIP_ACCENT,
    CHIP_IDLE,
    CHIP_LAYOUT,
    CHIP_OPEN,
    CHIP_PROBLEM,
  } from '../../../shared/ui/chip/chip-classes.ts';
  import Icon from '../../../shared/ui/icon/Icon.svelte';
  import type { IconName } from '../../../shared/ui/icon/icons.ts';
  import Popover from '../../../shared/ui/menu/Popover.svelte';
  import type { Composer, Menu } from './composer.svelte.ts';

  interface Props {
    readonly composer: Composer;
    readonly menu: Menu;
    readonly icon: IconName;
    readonly label: string;
    readonly title: string;
    readonly hint?: string;
    readonly reserve?: string;
    readonly problem: boolean;
    readonly accent?: boolean;
    readonly class: ClassValue;
    readonly children: Snippet;
  }

  const {
    composer,
    menu,
    icon,
    label,
    title,
    hint,
    reserve,
    problem,
    accent = false,
    class: className,
    children,
  }: Props = $props();
  const id = $props.id();
  const open = $derived(composer.menu === menu);
</script>

<div class="min-w-0 sm:relative" data-menu>
  <button
    {@attach composer.focus.attach(menu)}
    type="button"
    aria-haspopup="dialog"
    aria-expanded={open}
    aria-controls="{id}-{menu}"
    onclick={() => composer.toggleMenu(menu)}
    class={[
      'group/action',
      CHIP_LAYOUT,
      problem && !open ? CHIP_PROBLEM : accent ? CHIP_ACCENT : CHIP_IDLE,
      open && CHIP_OPEN,
    ]}
  >
    <Icon name={icon} class="size-4 shrink-0" animated />
    <span class="grid min-w-0">
      <span class="col-start-1 row-start-1 truncate">
        {#if hint !== undefined}<span class="sr-only">{hint}: </span>{/if}{label}
      </span>
      {#if reserve !== undefined}
        <span class="invisible col-start-1 row-start-1" aria-hidden="true">{reserve}</span>
      {/if}
    </span>
    <Icon
      name="chevronDown"
      stroke={2}
      class={['ml-auto size-3.5 shrink-0 opacity-70 transition-transform', open ? 'rotate-180' : 'rotate-0']}
    />
  </button>
  <Popover
    id="{id}-{menu}"
    label={title}
    {open}
    onclose={() => composer.closeMenu(menu)}
    class={['inset-x-0 top-full mt-2 p-1.5 sm:inset-x-auto sm:left-0', className]}
  >
    {@render children()}
  </Popover>
</div>
