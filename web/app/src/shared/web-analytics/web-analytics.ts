import { homePath, LANGUAGES } from '../language/language.ts';
import { OFFICIAL_ORIGIN } from '../local-client/instance-origin.ts';

const BEACON_URL = 'https://static.cloudflareinsights.com/beacon.min.js';
const BEACON_SETTINGS = JSON.stringify({ token: '218cf173bc814221a731915c8fb82088' });

const countsVisits =
  location.origin === OFFICIAL_ORIGIN && LANGUAGES.some((language) => homePath(language) === location.pathname);

// Only the CSP of these pages admits the policy; the entry module creates it before any other code can take the name.
function createBeaconPolicy() {
  try {
    return trustedTypes.createPolicy('nylzo-analytics', {
      createScriptURL(input) {
        if (input !== BEACON_URL) {
          throw new TypeError('unexpected analytics url');
        }
        return input;
      },
    });
  } catch {
    return null;
  }
}

const trustedTypesEnforced = typeof trustedTypes !== 'undefined';
const beaconPolicy = countsVisits && trustedTypesEnforced ? createBeaconPolicy() : undefined;

async function pageLoaded(): Promise<void> {
  if (document.readyState === 'complete') {
    return;
  }
  await new Promise<void>((resolve) => {
    addEventListener('load', () => resolve(), { once: true });
  });
}

export async function countVisit(): Promise<void> {
  if (!countsVisits || (trustedTypesEnforced && !beaconPolicy)) {
    return;
  }
  await pageLoaded();
  const script = document.createElement('script');
  script.type = 'module';
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- Keep the TrustedScriptURL object; the DOM declaration of src accepts only a string.
  script.src = (beaconPolicy?.createScriptURL(BEACON_URL) ?? BEACON_URL) as unknown as string;
  script.fetchPriority = 'low';
  script.dataset['cfBeacon'] = BEACON_SETTINGS;
  document.head.append(script);
}
