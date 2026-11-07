<script lang="ts">
  import type { Language } from './language.ts';

  interface Props {
    readonly language: Language;
  }

  const { language }: Props = $props();
</script>

<span class="inline-flex h-3.5 w-5 shrink-0 overflow-hidden rounded-[3px] ring-1 ring-white/15" aria-hidden="true">
  <svg viewBox="0 0 30 20" class="h-full w-full" preserveAspectRatio="xMidYMid slice" focusable="false">
    {#if language === 'cs'}
      <rect width="30" height="10" fill="#fff"></rect>
      <rect y="10" width="30" height="10" fill="#D7141A"></rect>
      <path d="M0 0L15 10L0 20Z" fill="#11457E"></path>
    {:else}
      <rect width="30" height="20" fill="#012169"></rect>
      <path d="M0 0L30 20M30 0L0 20" stroke="#fff" stroke-width="4"></path>
      <path d="M0 0L30 20M30 0L0 20" stroke="#C8102E" stroke-width="1.6"></path>
      <path d="M15 0V20M0 10H30" stroke="#fff" stroke-width="6"></path>
      <path d="M15 0V20M0 10H30" stroke="#C8102E" stroke-width="3.6"></path>
    {/if}
  </svg>
</span>
