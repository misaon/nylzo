<script lang="ts">
  interface Props {
    readonly pending: boolean;
    readonly text: string;
  }

  const { pending, text }: Props = $props();
</script>

{#if pending}
  <span class="inline-block h-5 w-20 animate-pulse rounded-xs bg-white/8 align-middle"></span>
{:else}
  {text}
{/if}
