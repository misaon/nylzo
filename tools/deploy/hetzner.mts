import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { appendFile, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { createImage, execute, initialize, inputs, prepare, importDns } from "./operator.mts";

const [command, kind, siteFile = ".deploy/site.json"] = process.argv.slice(2);
try {
  if (command === "init") {
    assert(kind, "Usage: deploy init SSH_PUBLIC_KEY [PRIVATE_DIRECTORY]");
    await initialize(kind, process.argv[4] ?? ".deploy");
  } else {
    assert(["plan", "apply", "outputs"].includes(command!), "Usage: deploy plan|apply|outputs image|server|dns [SITE_JSON]");
    const prepared = await prepare(siteFile, kind!);
    const { directory, environment } = prepared;
    const plan = path.join(directory, "deployment.tfplan");
    const planHash = async () => createHash("sha256").update(await readFile(plan)).digest("hex");
    if (command === "plan") {
      execute(directory, environment, ["init", "-input=false", "-lockfile=readonly"]);
      if (kind === "dns") await importDns(prepared);
      execute(directory, environment, ["plan", "-input=false", `-out=${plan}`]);
      await writeFile(path.join(directory, "review.json"), JSON.stringify({ inputs: await inputs(directory), plan: await planHash(), created: Date.now() }), { mode: 0o600 });
      const shown = JSON.parse(execute(directory, environment, ["show", "-json", plan], true));
      const changes = (shown.resource_changes ?? []).some((change: { change: { actions: string[] } }) => change.change.actions.some(action => action !== "no-op"));
      if (process.env.GITHUB_OUTPUT) await appendFile(process.env.GITHUB_OUTPUT, `changes=${changes}\n`);
      console.log(changes ? "Review this plan, then use deploy apply with the same arguments. Plans expire after one hour." : "The infrastructure already matches this configuration.");
    } else if (command === "apply") {
      // The plan hashed the zone imports, and CI applies on a fresh runner.
      if (kind === "dns") await importDns(prepared);
      const reviewed = JSON.parse(await readFile(path.join(directory, "review.json"), "utf8"));
      assert.equal(reviewed.inputs, await inputs(directory), "Configuration changed; create and review a new plan");
      assert.equal(reviewed.plan, await planHash(), "Saved plan changed");
      assert(Date.now() - reviewed.created >= 0 && Date.now() - reviewed.created <= 3600000, "Plan expired; create a fresh plan");
      execute(directory, environment, ["init", "-input=false", "-lockfile=readonly"]);
      execute(directory, environment, ["apply", "-input=false", plan]);
      if (kind === "image") await createImage(prepared);
      else execute(directory, environment, ["output"]);
    } else {
      execute(directory, environment, ["init", "-input=false", "-lockfile=readonly"]);
      execute(directory, environment, ["output"]);
    }
  }
} catch (error) {
  if (error instanceof assert.AssertionError) console.error(error.message.split("\n")[0]);
  else if (error instanceof Error && !("stdout" in error) && !("stderr" in error)) console.error(error.message);
  else console.error("Deployment operation failed. Inspect the preceding tool result; credentials and captured command output were omitted.");
  process.exitCode = 1;
}
