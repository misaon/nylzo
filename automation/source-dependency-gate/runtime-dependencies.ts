import assert from 'node:assert/strict';

import { arrayAt, objectAt, textAt } from './json-values.ts';
import type { JsonObject } from './json-values.ts';

export interface NpmRuntimeEvidence {
  readonly locked: readonly string[];
  readonly runtime: readonly string[];
}

interface LockedPackage {
  readonly id: string;
  readonly optional: readonly string[];
  readonly required: readonly string[];
}

interface Dependency {
  readonly name: string;
  readonly parentKey: string;
  readonly required: boolean;
}

const workspaceVersion = /@workspace:(?<directory>.+)$/u;

export function npmRuntimeEvidence(lockText: string): NpmRuntimeEvidence {
  const lock = objectAt(Bun.JSON5.parse(lockText), 'bun.lock');
  const workspaces = objectAt(lock['workspaces'], 'bun.lock workspaces');
  const packages = new Map(
    Object.entries(objectAt(lock['packages'], 'bun.lock packages')).map(([key, entry]) => [
      key,
      lockedPackage(entry, `bun.lock packages.${key}`),
    ]),
  );
  const pending: Dependency[] = Object.entries(workspaces).flatMap(([directory, workspace]) =>
    workspaceDependencies(workspace, directory),
  );
  const reached = new Set<string>();
  while (pending.length > 0) {
    const dependency = pending.pop();
    assert.ok(dependency !== undefined);
    const key = resolve(packages, dependency.parentKey, dependency.name);
    if (key === undefined) {
      assert.ok(!dependency.required, `Unresolved runtime dependency ${dependency.name} of "${dependency.parentKey}"`);
      continue;
    }
    if (reached.has(key)) {
      continue;
    }
    reached.add(key);
    const locked = packages.get(key);
    assert.ok(locked !== undefined);
    const directory = workspaceVersion.exec(locked.id)?.groups?.['directory'];
    if (directory !== undefined) {
      pending.push(...workspaceDependencies(workspaces[directory], directory, key));
      continue;
    }
    pending.push(
      ...locked.required.map((name) => ({ name, parentKey: key, required: true })),
      ...locked.optional.map((name) => ({ name, parentKey: key, required: false })),
    );
  }
  const idsOf = (keys: Iterable<string>): string[] =>
    [...new Set([...keys].map((key) => packages.get(key)?.id ?? key))].toSorted();
  return { locked: idsOf(packages.keys()), runtime: idsOf(reached) };
}

function workspaceDependencies(value: unknown, directory: string, packageKey?: string): Dependency[] {
  const workspace = objectAt(value, `bun.lock workspaces.${directory}`);
  const parentKey = packageKey ?? (directory === '' ? '' : textAt(workspace['name'], `workspaces.${directory}.name`));
  return [
    ...namesAt(workspace, 'dependencies').map((name) => ({ name, parentKey, required: true })),
    ...namesAt(workspace, 'optionalDependencies').map((name) => ({ name, parentKey, required: false })),
    ...namesAt(workspace, 'peerDependencies').map((name) => ({ name, parentKey, required: false })),
  ];
}

function lockedPackage(value: unknown, location: string): LockedPackage {
  const entry = arrayAt(value, location);
  const metadata = entry.find(
    (item): item is JsonObject => typeof item === 'object' && item !== null && !Array.isArray(item),
  );
  return {
    id: textAt(entry[0], `${location}[0]`),
    optional:
      metadata === undefined
        ? []
        : [...namesAt(metadata, 'optionalDependencies'), ...namesAt(metadata, 'peerDependencies')],
    required: metadata === undefined ? [] : namesAt(metadata, 'dependencies'),
  };
}

function namesAt(object: JsonObject, field: string): string[] {
  const value = object[field];
  return value === undefined ? [] : Object.keys(objectAt(value, field));
}

function resolve(packages: ReadonlyMap<string, LockedPackage>, parentKey: string, name: string): string | undefined {
  const scope = packagePath(parentKey);
  for (let length = scope.length; length >= 0; length -= 1) {
    const key = [...scope.slice(0, length), name].join('/');
    if (packages.has(key)) {
      return key;
    }
  }
  return undefined;
}

function packagePath(key: string): string[] {
  const segments = key === '' ? [] : key.split('/');
  const names: string[] = [];
  while (segments.length > 0) {
    const segment = segments.shift();
    assert.ok(segment !== undefined);
    names.push(segment.startsWith('@') ? `${segment}/${segments.shift() ?? ''}` : segment);
  }
  return names;
}
