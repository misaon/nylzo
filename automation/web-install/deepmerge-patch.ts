import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';

export interface PatchAssessment {
  readonly ecosystem: string;
  readonly id: string;
  readonly package: string;
  readonly sha256: string;
  readonly version: string;
}

export interface PatchEvidence extends PatchAssessment {
  readonly files: Readonly<Record<string, string>>;
}

type MergedObject = Record<PropertyKey, unknown>;

interface Deepmerge {
  (
    target: object,
    source: object,
    options?: { readonly arrayMerge: (target: readonly unknown[], source: readonly unknown[]) => readonly unknown[] },
  ): MergedObject;
  all(objects: readonly object[]): MergedObject;
}

export const deepmergePatch: PatchAssessment = {
  id: 'GHSA-gcqx-w48m-w2h4',
  ecosystem: 'npm',
  package: 'deepmerge',
  version: '4.3.1',
  sha256: 'c2cb33d5ff7ad8e752bdd9ec3a8e8e7520ad9b4da374307a6633372158ece14e',
};

const patchedFileHashes = {
  'index.js': '1799463abfdaef66f8ac03a90ec7bc9e5deadd968ea2ffe5ca7b6f057888e36e',
  'dist/cjs.js': '371951d3ebefbb374911bb51e763c08bf8523e55c2404eeb1aaea73211b00bbb',
  'dist/umd.js': '0aad2da89244af1b0aad6f57c60952eccac23d8dedb90593ddcbd11a7c2bd07f',
};

const repositoryRoot = path.resolve(import.meta.dirname, '../..');

export function isSamePatch(first: PatchAssessment, second: PatchAssessment): boolean {
  return (
    first.id === second.id &&
    first.ecosystem === second.ecosystem &&
    first.package === second.package &&
    first.version === second.version &&
    first.sha256 === second.sha256
  );
}

export function checkPatchBinding(patch: Buffer, workspaceManifest: string, lockfile: string): void {
  assert.equal(sha256(patch), deepmergePatch.sha256, 'Unreviewed deepmerge patch');
  const manifest: unknown = JSON.parse(workspaceManifest);
  const lock: unknown = Bun.JSON5.parse(lockfile);
  assert.ok(typeof manifest === 'object' && manifest !== null, 'Invalid web/package.json');
  assert.ok(typeof lock === 'object' && lock !== null, 'Invalid web/bun.lock');
  const patches = { 'deepmerge@4.3.1': 'patches/deepmerge@4.3.1.patch' };
  assert.deepEqual(Reflect.get(manifest, 'patchedDependencies'), patches, 'Manifest must apply the reviewed patch');
  assert.deepEqual(Reflect.get(lock, 'patchedDependencies'), patches, 'Lockfile must bind the reviewed patch');
  const packages: unknown = Reflect.get(lock, 'packages');
  assert.ok(typeof packages === 'object' && packages !== null, 'Missing locked packages');
  const consumers = Object.entries(packages).filter(
    ([name, entry]) =>
      name.split('/').at(-1) === 'deepmerge' || (Array.isArray(entry) && String(entry[0]).startsWith('deepmerge@')),
  );
  assert.ok(consumers.length > 0, 'No deepmerge package is locked');
  for (const [, entry] of consumers) {
    assert.ok(Array.isArray(entry), 'Invalid deepmerge lock entry');
    assert.equal(entry[0], 'deepmerge@4.3.1', 'Every deepmerge consumer must use the patched version');
    assert.equal(entry[1], '', 'Deepmerge must come from the registry');
  }
}

function checkMerge(merge: Deepmerge): void {
  for (const target of poisoningTargets()) {
    const results = [
      merge(target, poisonedPayload()),
      merge(target, { nested: poisonedPayload() }),
      merge(poisonedPayload(), target),
      merge.all([poisonedPayload(), target]),
    ];
    for (const result of results) {
      assertNotPoisoned(result, 'Returned object');
      if (typeof result['nested'] === 'object' && result['nested'] !== null) {
        assertNotPoisoned(result['nested'], 'Nested object');
      }
    }
  }
  assert.equal(Reflect.get(Object.prototype, 'nylzoPoisoned'), undefined, 'Object.prototype was poisoned');
  checkSvelteOptionMerging(merge);
}

export function verifyDeepmergePatch(root = repositoryRoot): PatchEvidence {
  const web = path.join(root, 'web');
  checkPatchBinding(
    readFileSync(path.join(web, 'patches/deepmerge@4.3.1.patch')),
    readFileSync(path.join(web, 'package.json'), 'utf8'),
    readFileSync(path.join(web, 'bun.lock'), 'utf8'),
  );
  const pluginRequire = requireFromSveltePlugin(web);
  const entry = pluginRequire.resolve('deepmerge');
  const packageDirectory = path.dirname(path.dirname(entry));
  assert.equal(entry, path.join(packageDirectory, 'dist/cjs.js'), 'Unexpected deepmerge entry point');
  assert.equal(installedVersion(packageDirectory), deepmergePatch.version, 'Unexpected installed deepmerge version');
  for (const [file, hash] of Object.entries(patchedFileHashes)) {
    const content = readFileSync(path.join(packageDirectory, file));
    assert.equal(sha256(content), hash, `Unpatched or changed deepmerge ${file}`);
  }
  const universalBuild = path.join(packageDirectory, 'dist/umd.js');
  const common: unknown = pluginRequire('deepmerge');
  const universal: unknown = pluginRequire(universalBuild);
  assert.ok(isDeepmerge(common) && isDeepmerge(universal), 'Invalid deepmerge API');
  checkMerge(common);
  checkMerge(universal);
  return { ...deepmergePatch, files: patchedFileHashes };
}

function requireFromSveltePlugin(webDirectory: string): NodeJS.Require {
  const appRequire = createRequire(path.join(webDirectory, 'app/package.json'));
  return createRequire(appRequire.resolve('@sveltejs/vite-plugin-svelte'));
}

function installedVersion(packageDirectory: string): unknown {
  const manifest: unknown = JSON.parse(readFileSync(path.join(packageDirectory, 'package.json'), 'utf8'));
  return typeof manifest === 'object' && manifest !== null ? Reflect.get(manifest, 'version') : undefined;
}

function poisoningTargets(): object[] {
  return [
    objectValue(Object.create(null)),
    { nested: null },
    { nested: undefined },
    objectValue(JSON.parse('{"__proto__":{}}')),
  ];
}

function poisonedPayload(): object {
  return objectValue(JSON.parse('{"__proto__":{"nylzoPoisoned":true},"safe":1}'));
}

function assertNotPoisoned(value: object, description: string): void {
  assert.equal(Reflect.get(value, 'nylzoPoisoned'), undefined, `${description} prototype was poisoned`);
  assert.equal(Object.hasOwn(value, '__proto__'), false, `${description} kept an own __proto__ key`);
}

function checkSvelteOptionMerging(merge: Deepmerge): void {
  const symbol = Symbol('option');
  const original = {
    nested: { a: 1 },
    list: [1],
    callback: identity,
    pattern: /svelte/u,
    date: new Date(0),
    [symbol]: true,
  };
  const result = merge(original, { nested: { b: 2 }, list: [2] }, { arrayMerge: (_target, source) => source });
  assert.deepEqual(result['nested'], { a: 1, b: 2 });
  assert.notEqual(result['nested'], original.nested);
  assert.deepEqual(result['list'], [2]);
  for (const key of ['callback', 'pattern', 'date', symbol] as const) {
    assert.equal(result[key], original[key], `deepmerge must keep option ${String(key)} by reference`);
  }
  assert.deepEqual(original.list, [1]);
}

function identity<Value>(value: Value): Value {
  return value;
}

function sha256(content: Buffer): string {
  return createHash('sha256').update(content).digest('hex');
}

function objectValue(value: unknown): object {
  assert.ok(typeof value === 'object' && value !== null, 'Expected a merge target object');
  return value;
}

function isDeepmerge(value: unknown): value is Deepmerge {
  return typeof value === 'function' && typeof Reflect.get(value, 'all') === 'function';
}
