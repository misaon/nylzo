import { forgetRetiredRecovery } from '../features/read/retired-recovery.ts';
import { browserDatabases } from '../shared/sent-records/indexed-database.ts';
import { purgeSentRecords } from '../shared/sent-records/sent-store.ts';

export { default as App } from './App.svelte';

export function prepare(): void {
  forgetRetiredRecovery(() => sessionStorage, browserDatabases());
}

export async function tidy(): Promise<void> {
  await purgeSentRecords(browserDatabases(), Date.now());
}
