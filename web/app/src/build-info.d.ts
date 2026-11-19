import type { BuildInfo } from './shared/build-info.ts';

declare global {
  const __NYLZO_BUILD_INFO__: BuildInfo;
  const __NYLZO_START_FAILURE__: Readonly<
    Partial<Record<string, { readonly message: string; readonly reopen: string; readonly retry: string }>>
  >;
}
