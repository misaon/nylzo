import assert from "node:assert/strict";
import { constants } from "node:fs";
import { open } from "node:fs/promises";

export async function privateFile(filename: string) {
  const handle = await open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const information = await handle.stat();
    assert(information.isFile() && information.size <= 8192, "Expected a small private file");
    assert.equal(information.mode & 0o077, 0, "Credential files must have mode 0600 or stricter");
    if (process.getuid) assert.equal(information.uid, process.getuid(), "Credential file belongs to another account");
    return (await handle.readFile("utf8")).trim();
  } finally { await handle.close(); }
}
