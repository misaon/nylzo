// nylzo admin-enroll prints 24 random bytes, which stay under the 41 characters the fragment guard lets through.
const ENROLLMENT_FRAGMENT = /^#enroll=([\w-]{32})$/u;

interface Address {
  readonly location: Pick<Location, 'hash' | 'pathname' | 'search'>;
  readonly history: Pick<History, 'replaceState'>;
}

export function takeEnrollmentToken({ location, history }: Address): string | undefined {
  if (location.hash === '') {
    return undefined;
  }
  const token = ENROLLMENT_FRAGMENT.exec(location.hash)?.[1];
  history.replaceState(null, '', `${location.pathname}${location.search}`);
  return token;
}
