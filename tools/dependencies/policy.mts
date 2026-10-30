import assert from "node:assert/strict";
import { versionParts } from "../release/policy.mts";

type Change = { name: string; before: string; after: string };

export function safeUpgrade(before: string, after: string): void {
  if (before === after) return;
  if (before.startsWith("jq-")) { assert(after.startsWith("jq-")); before = before.slice(3); after = after.slice(3); }
  const old = versionParts(before), next = versionParts(after);
  assert.equal(next[0], old[0], "Major dependency upgrades require attention");
  if (old[0] === 0) assert.equal(next[1], old[1], "Pre-1.0 minor upgrades require attention");
  assert(next.some((part, index) => part > old[index]! && next.slice(0, index).every((value, earlier) => value === old[earlier])), "Dependency downgrades require attention");
}

function masked(source: string, pattern: RegExp, changes: Change[], valueGroup = 1, name = "pin"): string {
  return source.replace(pattern, (...args: any[]) => {
    const match = args[0] as string, value = args[valueGroup] as string;
    changes.push({ name, before: value, after: value });
    const offset = match.indexOf(value);
    return match.slice(0, offset) + "DEPENDENCY_VALUE" + match.slice(offset + value.length);
  });
}

export function validateDependencyFile(path: string, before: string, after: string): Change[] {
  assert.notEqual(before, after);
  // One-way migration of the obsolete dependency annotation. Keep the actual
  // compatibility minimum subject to the same version/downgrade validation.
  const annotation = "# renovate: datasource=github-releases depName=jdx/mise\n";
  if (["mise.toml", "tools/security/runtime.toml"].includes(path) && !after.startsWith(annotation)) before = before.replace(/^# renovate: datasource=github-releases depName=jdx\/mise\n(?=min_version = ")/u, "");
  const changes: Change[] = [];
  if (/^web\/(?:[^/]+\/)?package\.json$/u.test(path) || path === "tools/release/package.json") {
    const old = JSON.parse(before), next = JSON.parse(after);
    for (const section of ["dependencies", "devDependencies", "optionalDependencies", "peerDependencies"]) {
      assert.deepEqual(Object.keys(next[section] ?? {}), Object.keys(old[section] ?? {}), "Adding or removing dependencies requires review");
      for (const [name, value] of Object.entries(old[section] ?? {})) {
        const updated = next[section][name];
        if (value !== updated) { safeUpgrade(value as string, updated); changes.push({ name, before: value as string, after: updated }); }
        next[section][name] = value;
      }
    }
    if (old.packageManager !== next.packageManager) {
      const from = /^pnpm@(\d+\.\d+\.\d+)$/u.exec(old.packageManager), to = /^pnpm@(\d+\.\d+\.\d+)$/u.exec(next.packageManager);
      assert(from && to); safeUpgrade(from[1]!, to[1]!);
      changes.push({ name: "pnpm", before: from[1]!, after: to[1]! }); next.packageManager = old.packageManager;
    }
    assert.deepEqual(next, old, "Scripts, registries and other package fields cannot change automatically");
    return changes;
  }
  if (path === "tools/release/package-lock.json") {
    const old = JSON.parse(before), next = JSON.parse(after);
    assert.equal(next.lockfileVersion, old.lockfileVersion); assert.equal(next.name, old.name);
    assert.deepEqual(Object.keys(next).sort(), Object.keys(old).sort());
    assert(next.packages && next.packages[""]);
    for (const [name, entry] of Object.entries(next.packages) as [string,any][]) {
      if (name === "") continue;
      assert(/^node_modules\/[A-Za-z0-9_@./-]+$/u.test(name) && !name.includes(".."));
      assert.equal(entry.link, undefined);
      assert(typeof entry.resolved === "string" && new URL(entry.resolved).origin === "https://registry.npmjs.org");
      assert.match(entry.integrity, /^sha(?:256|384|512)-[A-Za-z0-9+/]+=*$/u);
    }
    return [];
  }
  if (path === "web/pnpm-lock.yaml") {
    assert.equal(before.split(/\n(?:importers|packages):\n/u)[0], after.split(/\n(?:importers|packages):\n/u)[0]);
    // Registry deprecation messages can contain documentation links. Preserve
    // only warnings already reviewed in the base; do not exempt source fields.
    const oldLines = new Set(before.split("\n"));
    const sources = after.split("\n").filter(line => !(/^ +deprecated: /u.test(line) && oldLines.has(line))).join("\n");
    assert(!/\\|[&*][A-Za-z0-9_-]+/u.test(sources), "Escaped or aliased lockfile values require review");
    assert(!/(?:^|[\s,{])["']?(?:tarball|repo|commit)["']?\s*:/mu.test(sources), "Custom package sources require review");
    const externalSources = sources.replace(/\blink:\.\.\/core(?=$|[\s'",}])/gmu, "");
    assert(!/\b(?:[a-z][a-z0-9+.-]*:\/\/|git\+|git@|file:|link:|github:|gitlab:|bitbucket:)/iu.test(externalSources), "Custom lockfile sources require review");
    assert(/integrity: sha(?:256|384|512)-/u.test(after));
    return [];
  }
  if (path === "go.sum") {
    assert(after.split("\n").filter(Boolean).every(line => /^[A-Za-z0-9._~+!/-]+ v\S+(?:\/go\.mod)? h1:[A-Za-z0-9+/]{43}=$/u.test(line)));
    return [];
  }
  if (path === "mise.lock" || path === "tools/security/runtime.lock") {
    const normalize = (source: string) => {
      let tool = "", version = "", backend = "";
      return source.split("\n").map(line => {
        const section = /^\[\[tools\.([a-z0-9-]+)\]\]$/u.exec(line);
        if (section) { tool = section[1]!; version = ""; backend = ""; }
        if (line.startsWith("version = ")) { version = JSON.parse(line.slice(10)); versionParts(version); changes.push({ name: tool, before: version, after: version }); return "version = DEPENDENCY_VALUE"; }
        if (line.startsWith("backend = ")) backend = JSON.parse(line.slice(10));
        if (line.startsWith("specifiers = ")) { assert.deepEqual(JSON.parse(line.slice(13)), [version]); return "specifiers = DEPENDENCY_VALUE"; }
        if (line.startsWith("checksum = ")) { assert.match(JSON.parse(line.slice(11)), /^sha256:[a-f0-9]{64}$/u); return "checksum = DEPENDENCY_VALUE"; }
        if (line.startsWith("url = ") || line.startsWith("url_api = ")) {
          const url = new URL(JSON.parse(line.slice(line.indexOf("=") + 1)));
          assert.equal(url.protocol, "https:"); assert.equal(url.username + url.password + url.search + url.hash, "");
          const repo = backend.startsWith("aqua:") ? backend.slice(5) : undefined;
          if (line.startsWith("url_api")) assert(repo && url.hostname === "api.github.com" && url.pathname.startsWith(`/repos/${repo}/releases/assets/`));
          else if (repo) assert(url.hostname === "github.com" && url.pathname.startsWith(`/${repo}/releases/download/`));
          else if (backend === "core:go") assert.equal(url.hostname, "dl.google.com");
          else if (backend === "core:node") assert.equal(url.hostname, "nodejs.org");
          else assert.fail("Unrecognized lockfile backend");
          return line.slice(0, line.indexOf("=")) + "= DEPENDENCY_VALUE";
        }
        return line;
      }).join("\n");
    };
    const old = normalize(before), split = changes.length, next = normalize(after);
    assert.equal(next, old, "Lockfile tool identities and verification settings must stay fixed");
    const previous = changes.splice(0, split), updated = changes.splice(0);
    assert.equal(previous.length, updated.length);
    return previous.map((item, index) => { const value = updated[index]!; assert.equal(item.name, value.name); safeUpgrade(item.before, value.before); return { ...item, after: value.before }; });
  }
  if (path === "tools/dependencies/assets.json" || path === "deploy/hetzner-flatcar/toolchain.json") {
    const old = JSON.parse(before), next = JSON.parse(after);
    const walk = (left: any, right: any, key = "") => {
      assert.deepEqual(Object.keys(right), Object.keys(left));
      for (const name of Object.keys(left)) {
        if (typeof left[name] === "object") walk(left[name], right[name], `${key}${name}.`);
        else if (left[name] !== right[name]) {
          if (["sha256", "amd64", "arm64"].includes(name)) assert.match(right[name], /^[a-f0-9]{64}$/u);
          else { assert(["version", "flatcar", "caddy", "opentofu"].includes(name)); safeUpgrade(left[name], right[name]); changes.push({ name: key + name, before: left[name], after: right[name] }); }
          right[name] = left[name];
        }
      }
    };
    walk(old, next); assert.deepEqual(next, old); return changes;
  }
  const oldPins: Change[] = [], newPins: Change[] = [];
  let pattern: RegExp;
  if (path === "mise-tasks/deploy") pattern = /opentofu@(\d+\.\d+\.\d+)/gu;
  else if (path === "go.mod") pattern = /\b(v\d+\.\d+\.\d+|go\d+\.\d+\.\d+|\d+\.\d+(?:\.\d+)?)\b/gu;
  else if (path === "web/.node-version") pattern = /^(\d+\.\d+\.\d+)$/gmu;
  else if (/^(?:mise\.toml|tools\/security\/runtime\.toml|deploy\/hetzner-flatcar\/infrastructure\/(?:image|server|dns)\/main\.tf)$/u.test(path)) pattern = /(?:["=@]|\bgo)(v?\d+\.\d+\.\d+)(?=[" /]|$)/gmu;
  else if (/^deploy\/hetzner-flatcar\/infrastructure\/(?:image|server|dns)\/\.terraform\.lock\.hcl$/u.test(path)) {
    const normalize = (text: string) => text.replace(/(?:"(?:h1:[A-Za-z0-9+/]+=|zh:[a-f0-9]{64})"|"=? ?\d+\.\d+\.\d+")/gu, '"DEPENDENCY_VALUE"');
    assert.equal(normalize(before), normalize(after)); return [];
  } else if (path === ".goreleaser.yaml") {
    pattern = /(gcr\.io\/distroless\/static-debian13:nonroot@sha256:[a-f0-9]{64})/gu;
  } else if (path === "deploy/compose/compose.yaml") pattern = /(caddy:\d+\.\d+\.\d+@sha256:[a-f0-9]{64})/gu;
  else if (path === "tools/deploy/runtime.test.mts") pattern = /(ubuntu:\d+\.\d+@sha256:[a-f0-9]{64})/gu;
  else if (path === "tools/security/bootstrap-policy") pattern = /^([a-f0-9]{64})  /gmu;
  else if (/^\.github\/workflows\/[^/]+\.ya?ml$/u.test(path)) {
    const normalize = (text: string, values: Change[]) => {
      let result = text.replace(/(uses: [A-Za-z0-9_.-]+\/[A-Za-z0-9_./-]+@)([a-f0-9]{40})( # v?(\d+\.\d+\.\d+))/gu,
        (_all, prefix, _digest, _comment, version) => { values.push({ name: prefix, before: version, after: version }); return `${prefix}DEPENDENCY_SHA # DEPENDENCY_VERSION`; });
      result = result.replace(/(# renovate: datasource=[a-z-]+ depName=[A-Za-z0-9_./-]+\n[ \t]*(?:[a-z-]+:|node-version:)\s*)(v?\d+\.\d+\.\d+)/gu,
        (_all, prefix, version) => { values.push({ name: prefix.trim(), before: version, after: version }); return `${prefix}DEPENDENCY_VERSION`; });
      result = result.replace(/(go run golang\.org\/x\/vuln\/cmd\/govulncheck@)(v\d+\.\d+\.\d+)/gu,
        (_all, prefix, version) => { values.push({ name: prefix, before: version, after: version }); return `${prefix}DEPENDENCY_VERSION`; });
      result = result.replace(/(renovate-image: ghcr\.io\/renovatebot\/renovate:)(\d+\.\d+\.\d+)@sha256:[a-f0-9]{64}/gu,
        (_all, prefix, version) => { values.push({ name: prefix, before: version, after: version }); return `${prefix}DEPENDENCY_VERSION@DEPENDENCY_SHA`; });
      return result.replace(/echo '[a-f0-9]{64}  tools\/security\/bootstrap-policy'/gu, "echo 'DEPENDENCY_SHA  tools/security/bootstrap-policy'");
    };
    assert.equal(normalize(before, oldPins), normalize(after, newPins), "Workflow commands and permissions cannot change automatically");
    pattern = /NOT_A_REAL_PATTERN/gu;
  } else assert.fail(`Unmanaged dependency path: ${path}`);
  if (!oldPins.length && !newPins.length) assert.equal(masked(before, pattern!, oldPins), masked(after, pattern!, newPins), "Only dependency values may change");
  assert.equal(oldPins.length, newPins.length);
  for (let index = 0; index < oldPins.length; index++) {
    const old = oldPins[index]!, next = newPins[index]!;
    assert.equal(old.name, next.name);
    if (old.before === next.before) continue;
    if (old.before.startsWith("caddy:")) safeUpgrade(old.before.split(":")[1]!.split("@")[0]!, next.before.split(":")[1]!.split("@")[0]!);
    else if (old.before.startsWith("ubuntu:")) assert.equal(old.before.split("@")[0], next.before.split("@")[0], "Test image OS upgrades require attention");
    else if (/^[a-f0-9]{64}$/u.test(old.before) || old.before.startsWith("gcr.io/")) { /* verified digests */ }
    else safeUpgrade(old.before.replace(/^go/u, ""), next.before.replace(/^go/u, ""));
    changes.push({ name: old.name, before: old.before, after: next.before });
  }
  return changes;
}
