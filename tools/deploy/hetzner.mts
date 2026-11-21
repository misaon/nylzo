import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { appendFile, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { isRecord } from "./configuration.mts";
import { createImage, execute, initialize, inputs, isStack, prepare, importDns, workspace } from "./operator.mts";

type Review = { inputs: string; plan: string; created: number };

function isReview(value: unknown): value is Review {
  return isRecord(value) && typeof value["inputs"] === "string" && typeof value["plan"] === "string" && typeof value["created"] === "number";
}

function planChanges(shown: unknown) {
  assert(isRecord(shown), "OpenTofu printed a plan this tool cannot read");
  const resourceChanges = shown["resource_changes"] ?? [];
  assert(Array.isArray(resourceChanges), "OpenTofu printed a plan this tool cannot read");
  return resourceChanges.some((resourceChange: unknown) => {
    const change = isRecord(resourceChange) ? resourceChange["change"] : undefined;
    const actions = isRecord(change) ? change["actions"] : undefined;
    assert(Array.isArray(actions), "OpenTofu printed a plan this tool cannot read");
    return actions.some(action => action !== "no-op");
  });
}

const [command, kind, siteFile = ".deploy/site.json"] = process.argv.slice(2);
try {
  if (command === "init") {
    assert(kind, "Usage: deploy init SSH_PUBLIC_KEY [PRIVATE_DIRECTORY]");
    await initialize(kind, process.argv[4] ?? ".deploy");
  } else {
    assert(command !== undefined && ["plan", "apply", "outputs", "migrate"].includes(command) && isStack(kind), "Usage: deploy plan|apply|outputs|migrate image|server|dns [SITE_JSON]");
    assert(command !== "migrate" || process.env["NYLZO_STATE_BUCKET"], "Set NYLZO_STATE_BUCKET, NYLZO_STATE_ENDPOINT and NYLZO_STATE_REGION to the bucket that receives the state");
    // Moving state needs neither the release bundle nor a verified image of the pinned Flatcar.
    const prepared = command === "migrate" ? await workspace(siteFile, kind) : await prepare(siteFile, kind);
    const { directory, environment } = prepared;
    const plan = path.join(directory, "deployment.tfplan");
    const planHash = async () => createHash("sha256").update(await readFile(plan)).digest("hex");
    const initializeStack = (...options: string[]) => execute(directory, environment, ["init", "-input=false", "-lockfile=readonly", ...options]);
    if (command === "plan") {
      initializeStack();
      if (kind === "dns") await importDns(prepared);
      execute(directory, environment, ["plan", "-input=false", `-out=${plan}`]);
      await writeFile(path.join(directory, "review.json"), JSON.stringify({ inputs: await inputs(directory), plan: await planHash(), created: Date.now() } satisfies Review), { mode: 0o600 });
      const changes = planChanges(JSON.parse(execute(directory, environment, ["show", "-json", plan], true)));
      if (process.env["GITHUB_OUTPUT"]) await appendFile(process.env["GITHUB_OUTPUT"], `changes=${changes}\n`);
      console.log(changes ? "Review this plan, then use deploy apply with the same arguments. Plans expire after one hour." : "The infrastructure already matches this configuration.");
    } else if (command === "apply") {
      // The plan hashed the zone imports, and CI applies on a fresh runner.
      if (kind === "dns") await importDns(prepared);
      const reviewed: unknown = JSON.parse(await readFile(path.join(directory, "review.json"), "utf8"));
      assert(isReview(reviewed), "The saved review is unreadable; create and review a new plan");
      assert.equal(reviewed.inputs, await inputs(directory), "Configuration changed; create and review a new plan");
      assert.equal(reviewed.plan, await planHash(), "Saved plan changed");
      assert(Date.now() - reviewed.created >= 0 && Date.now() - reviewed.created <= 3600000, "Plan expired; create a fresh plan");
      initializeStack();
      execute(directory, environment, ["apply", "-input=false", plan]);
      if (kind === "image") await createImage(prepared);
      else execute(directory, environment, ["output"]);
    } else if (command === "migrate") {
      initializeStack("-migrate-state", "-force-copy");
    } else {
      initializeStack();
      execute(directory, environment, ["output"]);
    }
  }
} catch (error) {
  if (error instanceof assert.AssertionError) console.error(error.message.split("\n")[0]);
  else if (error instanceof Error && !("stdout" in error) && !("stderr" in error)) console.error(error.message);
  else console.error("Deployment operation failed. Inspect the preceding tool result; credentials and captured command output were omitted.");
  process.exitCode = 1;
}
