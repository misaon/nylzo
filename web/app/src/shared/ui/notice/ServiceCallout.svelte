<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue } from 'svelte/elements';

  import Icon from '../icon/Icon.svelte';
  import { CALLOUT, CALLOUT_BADGE, CALLOUT_MESSAGE } from './callout-classes.ts';

  interface Props {
    readonly id?: string;
    readonly message: string;
    readonly class?: ClassValue | undefined;
    readonly action?: Snippet;
  }

  const { id, message, class: className, action }: Props = $props();
</script>

<div class={[CALLOUT, className]}>
  <span class={CALLOUT_BADGE} aria-hidden="true">
    <Icon name="caution" class="size-4.5" stroke={1.9} />
  </span>
  <p {id} role="alert" class={CALLOUT_MESSAGE}>{message}</p>
  {@render action?.()}
</div>
