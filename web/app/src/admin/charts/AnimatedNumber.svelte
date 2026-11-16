<script lang="ts">
  import { tweenedNumber } from './motion.svelte.ts';

  interface Props {
    readonly value: number;
    readonly format: (value: number) => string;
  }

  const { value, format }: Props = $props();
  const shown = tweenedNumber(() => value);
</script>

<span aria-hidden="true">{format(shown.current)}</span><span class="sr-only">{format(value)}</span>
