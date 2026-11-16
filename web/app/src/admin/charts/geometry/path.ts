export interface Point {
  readonly x: number;
  readonly y: number;
}

function round(value: number): number {
  return Math.round(value * 100) / 100;
}

function coordinates(point: Point): string {
  return `${round(point.x)},${round(point.y)}`;
}

function interiorTangent(before: Point, at: Point, after: Point): number {
  const leftWidth = at.x - before.x;
  const rightWidth = after.x - at.x;
  const leftSlope = leftWidth === 0 ? 0 : (at.y - before.y) / leftWidth;
  const rightSlope = rightWidth === 0 ? 0 : (after.y - at.y) / rightWidth;
  const blended = (leftSlope * rightWidth + rightSlope * leftWidth) / (leftWidth + rightWidth);
  const tangent =
    (Math.sign(leftSlope) + Math.sign(rightSlope)) *
    Math.min(Math.abs(leftSlope), Math.abs(rightSlope), 0.5 * Math.abs(blended));
  return Number.isFinite(tangent) ? tangent : 0;
}

function endTangent(from: Point, to: Point, neighbour: number): number {
  const width = to.x - from.x;
  return width === 0 ? neighbour : (3 * (to.y - from.y)) / width / 2 - neighbour / 2;
}

function tangents(points: readonly Point[]): number[] {
  const slopes = points.map((point, i) => {
    const before = points[i - 1];
    const after = points[i + 1];
    return before && after ? interiorTangent(before, point, after) : 0;
  });
  const [first, second] = points;
  const last = points.at(-1);
  const beforeLast = points.at(-2);
  if (first && second && last && beforeLast) {
    slopes[0] = endTangent(first, second, slopes[1] ?? 0);
    slopes[points.length - 1] = endTangent(beforeLast, last, slopes.at(-2) ?? 0);
  }
  return slopes;
}

export function smoothLine(points: readonly Point[]): string {
  const [first, second] = points;
  if (!first) {
    return '';
  }
  if (!second) {
    return `M${coordinates(first)}`;
  }
  if (points.length === 2) {
    return `M${coordinates(first)}L${coordinates(second)}`;
  }
  const slopes = tangents(points);
  let path = `M${coordinates(first)}`;
  for (let i = 1; i < points.length; i += 1) {
    const from = points[i - 1];
    const to = points[i];
    if (!from || !to) {
      continue;
    }
    const third = (to.x - from.x) / 3;
    const start = { x: from.x + third, y: from.y + third * (slopes[i - 1] ?? 0) };
    const end = { x: to.x - third, y: to.y - third * (slopes[i] ?? 0) };
    path += `C${coordinates(start)} ${coordinates(end)} ${coordinates(to)}`;
  }
  return path;
}

export function smoothArea(points: readonly Point[], baseline: number): string {
  const first = points[0];
  const last = points.at(-1);
  if (!first || !last || points.length < 2) {
    return '';
  }
  return `${smoothLine(points)}L${coordinates({ x: last.x, y: baseline })}L${coordinates({ x: first.x, y: baseline })}Z`;
}
