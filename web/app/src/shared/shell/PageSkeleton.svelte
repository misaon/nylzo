<script lang="ts">
  import Logo from './Logo.svelte';
</script>

<div class="relative min-h-dvh bg-void-950" data-skeleton aria-busy="true">
  <div class="px-3 pt-3 sm:px-6">
    <div class="glass-soft mx-auto flex h-14 max-w-page items-center rounded-card pr-2 pl-4">
      <Logo class="h-7 w-[105px]" />
    </div>
  </div>
  <div class="grid place-items-center pt-[28vh] text-aegis-300" aria-hidden="true">
    <svg
      class="size-8 motion-safe:animate-spin"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.8"
      stroke-linecap="round"
    >
      <circle cx="12" cy="12" r="9" stroke-opacity="0.2"></circle>
      <path d="M21 12a9 9 0 0 0-9-9"></path>
    </svg>
  </div>
</div>
