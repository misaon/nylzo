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

interface Knot {
  readonly point: Point;
  readonly slope: number;
}

function curve(from: Knot, to: Knot): string {
  const third = (to.point.x - from.point.x) / 3;
  const start = { x: from.point.x + third, y: from.point.y + third * from.slope };
  const end = { x: to.point.x - third, y: to.point.y - third * to.slope };
  return `C${coordinates(start)} ${coordinates(end)} ${coordinates(to.point)}`;
}

export function smoothLine(points: readonly Point[]): string {
  const [first, second, third] = points;
  if (!first) {
    return '';
  }
  if (!second) {
    return `M${coordinates(first)}`;
  }
  if (!third) {
    return `M${coordinates(first)}L${coordinates(second)}`;
  }
  const firstInner = { point: second, slope: interiorTangent(first, second, third) };
  const inner = [firstInner];
  let lastInner = firstInner;
  let before = second;
  let at = third;
  for (const after of points.slice(3)) {
    lastInner = { point: at, slope: interiorTangent(before, at, after) };
    inner.push(lastInner);
    before = at;
    at = after;
  }
  let from = { point: first, slope: endTangent(first, second, firstInner.slope) };
  let path = `M${coordinates(first)}`;
  for (const to of [...inner, { point: at, slope: endTangent(before, at, lastInner.slope) }]) {
    path += curve(from, to);
    from = to;
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
