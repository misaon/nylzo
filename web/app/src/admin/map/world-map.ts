export interface WorldMap {
  readonly width: number;
  readonly height: number;
  readonly source: string;
  readonly countries: Readonly<Record<string, string>>;
}

export async function loadWorldMap(): Promise<WorldMap> {
  const module = await import('./world-map.json');
  return module.default;
}
