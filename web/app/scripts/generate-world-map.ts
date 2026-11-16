import { writeFileSync } from 'node:fs';
import path from 'node:path';

import { geoEqualEarth, geoPath } from 'd3-geo';
import countries from 'i18n-iso-countries';
import { feature } from 'topojson-client';
import type { GeometryCollection, Topology } from 'topojson-specification';
import atlas from 'world-atlas/countries-110m.json' with { type: 'json' };
import atlasPackage from 'world-atlas/package.json' with { type: 'json' };

interface CountryProperties {
  readonly name: string;
}

const output = path.resolve(import.meta.dirname, '../src/admin/map/world-map.json');
const width = 1000;
const antarcticaId = '010';
const kosovoName = 'Kosovo';

// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- world-atlas ships untyped TopoJSON with the countries object.
const topology = atlas as unknown as Topology<{ countries: GeometryCollection<CountryProperties> }>;
const collection = feature(topology, topology.objects.countries);

const inhabited = {
  ...collection,
  features: collection.features.filter((country) => country.id !== antarcticaId),
};

const projection = geoEqualEarth().fitWidth(width, inhabited);
const toPath = geoPath(projection).digits(1);
const [, [, bottom]] = toPath.bounds(inhabited);
const height = Math.ceil(bottom);

function codeOf(name: string, id: string | number | undefined): string | undefined {
  if (name === kosovoName) {
    return 'XK';
  }
  return typeof id === 'string' ? countries.numericToAlpha2(id) : undefined;
}

const paths = new Map<string, string>();
let unnamedCount = 0;
const ordered = inhabited.features.toSorted((first, second) =>
  first.properties.name.localeCompare(second.properties.name, 'en'),
);
for (const country of ordered) {
  const outline = toPath(country);
  if (outline === null) {
    continue;
  }
  let code = codeOf(country.properties.name, country.id);
  if (code === undefined) {
    unnamedCount += 1;
    code = `_${unnamedCount}`;
  }
  paths.set(code, `${paths.get(code) ?? ''}${outline}`);
}

const sorted = Object.fromEntries([...paths].toSorted(([first], [second]) => (first < second ? -1 : 1)));
const data = JSON.stringify(
  { width, height, source: `Natural Earth 1:110m via world-atlas ${atlasPackage.version}`, countries: sorted },
  undefined,
  2,
);
writeFileSync(output, `${data}\n`);
console.log(`wrote ${paths.size} outlines, ${unnamedCount} unnamed, ${data.length} bytes, height ${height}`);
