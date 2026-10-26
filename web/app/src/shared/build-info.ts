import { REPOSITORY_URL } from './shell/repository.ts';

export interface BuildInfo {
  readonly version: string;
  readonly commit: string;
  readonly date: string;
  readonly dirty: boolean;
}

export const BUILD_INFO: BuildInfo = __NYLZO_BUILD_INFO__;

export function buildSourceHref(build: BuildInfo): string | undefined {
  if (!build.dirty && /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(build.version)) {
    return `${REPOSITORY_URL}/releases/tag/v${build.version}`;
  }
  if (/^[\da-f]{7,40}$/i.test(build.commit)) {
    return `${REPOSITORY_URL}/commit/${build.commit}`;
  }
  return undefined;
}
