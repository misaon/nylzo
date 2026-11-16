import type { czech } from './access.cs.ts';

export const english: Readonly<Record<keyof typeof czech, string>> = {
  'access.area': 'Admin',
  'access.back': 'Back to the site',
  'access.loading': 'Loading the admin area…',
  'access.expires': 'Session until {time}',
  'access.signOut': 'Sign out',
  'access.retry': 'Try again',
  'access.copy': 'Copy',
  'access.copied': 'Copied',
  'access.copy.label': 'Copy the command: {command}',

  'access.unconfigured.title': 'The admin area is not set up yet',
  'access.unconfigured.text':
    'Run the command below on the server and open the link it prints. The link works once and for 15 minutes.',
  'access.unconfigured.binary': 'On the server',
  'access.unconfigured.docker': 'With Docker Compose',
  'access.unconfigured.counting': 'Until the admin area has a passkey, the server counts no visits or messages.',

  'access.enroll.title': 'Create a passkey for the admin area',
  'access.enroll.text':
    'This device or your password manager keeps the passkey, and it signs you in right after. The link from the command works only once.',
  'access.enroll.action': 'Create passkey',

  'access.signIn.title': 'Sign in to the admin area',
  'access.signIn.text':
    'Sign in with the passkey you created for the admin area. The session lives only in this tab; after a reload you sign in again.',
  'access.signIn.action': 'Sign in with passkey',
  'access.waiting': 'Waiting for the passkey…',

  'access.unsupported.title': 'This browser cannot use passkeys here',
  'access.unsupported.text':
    'The admin area needs passkeys and Ed25519 signatures in the browser. Open it in the current version of your browser.',
  'access.unavailable.title': 'The admin area did not load',

  'access.notice.enrolled': 'The passkey is ready. Sign in with it.',
  'access.notice.ended': 'The session ended. Sign in again.',
  'access.notice.signedOut': 'You are signed out.',

  'access.problem.cancelled': 'The passkey was not used. Try again.',
  'access.problem.not-allowed':
    'The browser did not allow a passkey at this address. Open the admin area at the public address of the server.',
  'access.problem.unsupported': 'This browser or device does not support such a passkey.',
  'access.problem.key-type':
    'The browser did not hand over the public key of the passkey. Try another browser or password manager.',
  'access.problem.already-enrolled': 'This device already holds a passkey for the admin area. Sign in with it.',
  'access.problem.link':
    'The link was already used, has expired or a newer one replaced it. Run nylzo admin-enroll on the server again.',
  'access.problem.refused':
    'The server does not know this passkey. Use another one or add a new one with nylzo admin-enroll.',
  'access.problem.busy': 'The server is busy right now. Try again in a moment.',
  'access.problem.offline': 'The server does not answer. Check the connection and try again.',
  'access.problem.failed': 'Something went wrong. Try again.',
};
