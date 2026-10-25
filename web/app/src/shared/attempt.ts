export async function attempt<Result>(work: Promise<Result>): Promise<Result | undefined> {
  try {
    return await work;
  } catch {
    return undefined;
  }
}
