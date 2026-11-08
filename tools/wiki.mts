import { mkdir, readdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const destination = path.join(root, "dist/wiki");
const repository = "https://github.com/misaon/nylzo";
const wiki = `${repository}/wiki`;
const pages = (await readdir(path.join(root, "docs/wiki"), { recursive: true }))
  .filter(file => file.endsWith(".md"))
  .sort()
  .map(file => [path.join("docs/wiki", file), path.basename(file, ".md")] as const);
const sources = new Map([...pages, ["CODE_OF_CONDUCT.md", "Code-of-conduct"]]);
const duplicates = [...sources.values()].filter((page, index, all) => all.indexOf(page) !== index);
if (duplicates.length > 0) {
  throw new Error(`Two Wiki sources map to the same page: ${duplicates.join(", ")}`);
}

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
