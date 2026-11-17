import { writeFileSync } from 'node:fs';
import path from 'node:path';

import { geoEqualEarth, geoPath } from 'd3-geo';
import type { GeoContext } from 'd3-geo';
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
const [, [, bottom]] = geoPath(projection).bounds(inhabited);
const height = Math.ceil(bottom);

function tenths(coordinate: number): number {
  return Math.round(coordinate * 10);
}

function decimal(tenthsOfUnit: number): string {
  return String(tenthsOfUnit / 10).replace(/^(-?)0\./u, '$1.');
}

function joined(numbers: readonly string[]): string {
  return numbers
    .map((number, i) => {
      const previous = numbers[i - 1];
      const separated =
        previous !== undefined && !number.startsWith('-') && !(number.startsWith('.') && previous.includes('.'));
      return separated ? ` ${number}` : number;
    })
    .join('');
}

// Steps between whole tenths keep every point where a one-decimal absolute path puts it.
class RelativeOutline implements GeoContext {
  #path = '';
  #steps: string[] = [];
  #x = 0;
  #y = 0;

  beginPath(): void {
    this.#path = '';
    this.#steps = [];
  }

  moveTo(x: number, y: number): void {
    this.#endSteps();
    this.#x = tenths(x);
    this.#y = tenths(y);
    this.#path += `M${joined([decimal(this.#x), decimal(this.#y)])}`;
  }

  lineTo(x: number, y: number): void {
    const nextX = tenths(x);
    const nextY = tenths(y);
    this.#steps.push(decimal(nextX - this.#x), decimal(nextY - this.#y));
    this.#x = nextX;
    this.#y = nextY;
  }

  closePath(): void {
    this.#endSteps();
    this.#path += 'z';
  }

  arc(): void {
    throw new Error('a country outline has no point geometry');
  }

  take(): string {
    this.#endSteps();
    const shape = this.#path;
    this.beginPath();
    return shape;
  }

  #endSteps(): void {
    if (this.#steps.length > 0) {
      this.#path += `l${joined(this.#steps)}`;
      this.#steps = [];
    }
  }
}

const outline = new RelativeOutline();
const drawOutline = geoPath(projection, outline);

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
  drawOutline(country);
  const shape = outline.take();
  if (shape === '') {
    continue;
  }
  let code = codeOf(country.properties.name, country.id);
  if (code === undefined) {
    unnamedCount += 1;
    code = `_${unnamedCount}`;
  }
  paths.set(code, `${paths.get(code) ?? ''}${shape}`);
}

const sorted = Object.fromEntries([...paths].toSorted(([first], [second]) => (first < second ? -1 : 1)));
const data = JSON.stringify(
  { width, height, source: `Natural Earth 1:110m via world-atlas ${atlasPackage.version}`, countries: sorted },
  undefined,
  2,
);
writeFileSync(output, `${data}\n`);
console.log(`wrote ${paths.size} outlines, ${unnamedCount} unnamed, ${data.length} bytes, height ${height}`);
