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
    options?: { arrayMerge: (target: unknown[], source: unknown[]) => unknown[] },
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
  const workspace = workspaceManifest.replaceAll('\r\n', '\n');
  const lock = lockfile.replaceAll('\r\n', '\n');
  assert.match(
    workspace,
    /^patchedDependencies:\n {2}deepmerge@4\.3\.1: patches\/deepmerge@4\.3\.1\.patch$/m,
    'web/pnpm-workspace.yaml must apply the reviewed deepmerge patch',
  );
  assert.match(
    lock,
    new RegExp(String.raw`^patchedDependencies:\n {2}deepmerge@4\.3\.1: ${deepmergePatch.sha256}$`, 'm'),
    'web/pnpm-lock.yaml must record the reviewed deepmerge patch',
  );
  const consumers = Array.from(lock.matchAll(/^\s+deepmerge: (.+)$/gm), (match) => match[1]);
  assert.deepEqual(
    consumers,
    [`4.3.1(patch_hash=${deepmergePatch.sha256})`],
    'Every deepmerge consumer must use the reviewed patch',
  );
  assert.doesNotMatch(lock, /^ {2}deepmerge@4\.3\.1: \{\}$/m, 'Unpatched deepmerge snapshot');
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
    readFileSync(path.join(web, 'pnpm-workspace.yaml'), 'utf8'),
    readFileSync(path.join(web, 'pnpm-lock.yaml'), 'utf8'),
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
  checkMerge(pluginRequire('deepmerge') as Deepmerge);
  checkMerge(pluginRequire(universalBuild) as Deepmerge);
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
    Object.create(null) as object,
    { nested: null },
    { nested: undefined },
    JSON.parse('{"__proto__":{}}') as object,
  ];
}

function poisonedPayload(): object {
  return JSON.parse('{"__proto__":{"nylzoPoisoned":true},"safe":1}') as object;
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
    pattern: /svelte/,
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
