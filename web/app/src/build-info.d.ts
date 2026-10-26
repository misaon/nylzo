import type { BuildInfo } from './shared/build-info.ts';

declare global {
  const __NYLZO_BUILD_INFO__: BuildInfo;
}
