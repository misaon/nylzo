import { mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const destination = path.join(root, "dist/wiki");
const repository = "https://github.com/misaon/nylzo";
const wiki = `${repository}/wiki`;
const sources = new Map([
  ["docs/wiki/Home.md", "Home"],
  ["docs/wiki/guides/Getting-started.md", "Getting-started"],
  ["docs/wiki/guides/Self-hosting.md", "Self-hosting"],
  ["docs/wiki/guides/Security-model.md", "Security-model"],
  ["docs/wiki/guides/Verify-a-release.md", "Verify-a-release"],
  ["docs/wiki/guides/Development.md", "Development"],
  ["docs/wiki/reference/Repository-map.md", "Repository-map"],
  ["docs/wiki/guides/Name-and-logo.md", "Name-and-logo"],
  ["docs/wiki/guides/CLI.md", "CLI"],
  ["docs/wiki/guides/Operations.md", "Operations"],
  ["docs/wiki/maintainers/Releasing.md", "Releasing"],
  ["docs/wiki/maintainers/Release-checklist.md", "Release-checklist"],
  ["docs/wiki/maintainers/Repository-governance.md", "Repository-governance"],
  ["docs/wiki/maintainers/Security-checks.md", "Security-checks"],
  ["docs/wiki/maintainers/Security-updates.md", "Security-updates"],
  ["docs/wiki/community/Contributing.md", "Contributing"],
  ["docs/wiki/community/Security-policy.md", "Security-policy"],
  ["docs/wiki/reference/Protocol.md", "Protocol"],
  ["docs/wiki/guides/Brand-assets.md", "Brand-assets"],
  ["docs/wiki/maintainers/Publishing.md", "Publishing"],
  ["CODE_OF_CONDUCT.md", "Code-of-conduct"],
  ["docs/wiki/guides/Fonts.md", "Fonts"],
  ["docs/wiki/_Sidebar.md", "_Sidebar"],
]);

await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
for (const [source, page] of sources) {
  let markdown = await readFile(path.join(root, source), "utf8");
  const links = [...markdown.matchAll(/!?\[[^\]\n]*\]\(([^)\s]+)\)/gu)];
  for (const match of links.reverse()) {
    const target = match[1];
    let replacement = target;
    if (target.startsWith(`${wiki}/`)) {
      const name = new URL(target).pathname.split("/").at(-1);
      if (![...sources.values()].includes(name!)) {
        throw new Error(`${source}: unknown Wiki page ${target}`);
      }
    } else if (!/^(?:[a-z]+:|#)/iu.test(target)) {
      const [filename, fragment = ""] = target.split("#");
      const resolved = path.resolve(root, path.dirname(source), filename!);
      const relative = path.relative(root, resolved);
      if (relative.startsWith("..") || path.isAbsolute(relative)) {
        throw new Error(`${source}: link escapes the repository: ${target}`);
      }
      const information = await stat(resolved);
      const mapped = sources.get(relative);
      const encoded = relative.split(path.sep).map(encodeURIComponent).join("/");
      replacement = mapped
        ? `${wiki}/${mapped}`
        : `${repository}/${information.isDirectory() ? "tree" : "blob"}/main/${encoded}`;
      if (fragment) replacement += `#${fragment}`;
    }
    const index = match.index + match[0].indexOf("](") + 2;
    markdown = markdown.slice(0, index) + replacement + markdown.slice(index + target.length);
  }
  if (page !== "_Sidebar") {
    markdown =
      markdown.trimEnd() + `\n\n---\n\n[Edit this page through a pull request](${repository}/blob/main/${source}).\n`;
  }
  await writeFile(path.join(destination, `${page}.md`), markdown);
}
await writeFile(
  path.join(destination, "_Footer.md"),
  `[Nylzo](${repository}) · [Releases](${repository}/releases) · [Contribute](${wiki}/Contributing) · [Report a vulnerability privately](${repository}/security/advisories/new)\n`,
);
console.log(`Rendered ${sources.size + 1} Wiki files in ${destination}`);
