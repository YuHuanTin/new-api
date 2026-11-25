
export type FoilPoint = { x: number; y: number }
export type FoilEdge = {
  p: FoilPoint
  edge: number
  t: number
  distance: number
}

export const SEALED_CARD: FoilPoint[] = [
  { x: 0, y: 0 },
  { x: 100, y: 0 },
  { x: 100, y: 145 },
  { x: 0, y: 145 },
]

export function foilArea(polygon: FoilPoint[]) {
  let sum = 0
  for (const [i, a] of polygon.entries()) {
    const b = polygon[(i + 1) % polygon.length]
    sum += a.x * b.y - b.x * a.y
  }
  return Math.abs(sum) / 2
}

export function insideFoil(polygon: FoilPoint[], point: FoilPoint) {
  let inside = false
  for (const [i, a] of polygon.entries()) {
    const b = polygon[(i + 1) % polygon.length]
    if (a.y > point.y === b.y > point.y) continue
    if (point.x < a.x + ((b.x - a.x) * (point.y - a.y)) / (b.y - a.y)) {
      inside = !inside
    }
  }
  return inside
}

export function nearestFoilEdge(
  polygon: FoilPoint[],
  point: FoilPoint
): FoilEdge {
  let nearest: FoilEdge = { p: polygon[0], edge: 0, t: 0, distance: Infinity }
  for (const [i, a] of polygon.entries()) {
    const b = polygon[(i + 1) % polygon.length]
    const dx = b.x - a.x
    const dy = b.y - a.y
    const along =
      ((point.x - a.x) * dx + (point.y - a.y) * dy) / (dx * dx + dy * dy || 1)
    const t = Math.min(1, Math.max(0, along))
    const p = { x: a.x + dx * t, y: a.y + dy * t }
    const distance = Math.hypot(point.x - p.x, point.y - p.y)
    if (distance < nearest.distance) nearest = { p, edge: i, t, distance }
  }
  return nearest
}

export function foilCrossing(
  a: FoilPoint,
  b: FoilPoint,
  c: FoilPoint,
  d: FoilPoint
) {
  const rx = b.x - a.x
  const ry = b.y - a.y
  const sx = d.x - c.x
  const sy = d.y - c.y
  const den = rx * sy - ry * sx
  if (Math.abs(den) < 1e-9) return null
  const x = c.x - a.x
  const y = c.y - a.y
  const t = (x * sy - y * sx) / den
  const u = (x * ry - y * rx) / den
  return t >= 0 && t <= 1 && u >= 0 && u <= 1 ? { t, u } : null
}

function walkFoil(polygon: FoilPoint[], from: FoilEdge, to: FoilEdge) {
  if (from.edge === to.edge && to.t >= from.t) return []
  const points: FoilPoint[] = []
  for (
    let i = (from.edge + 1) % polygon.length;
    ;
    i = (i + 1) % polygon.length
  ) {
    points.push(polygon[i])
    if (i === to.edge) return points
  }
}

export function splitFoil(
  polygon: FoilPoint[],
  from: FoilEdge,
  path: FoilPoint[],
  to: FoilEdge
) {
  const line = [from.p, ...path, to.p]
  const first = [...line, ...walkFoil(polygon, to, from)]
  const second = [...line.reverse(), ...walkFoil(polygon, from, to)]
  return foilArea(first) < foilArea(second)
    ? { piece: first, body: second }
    : { piece: second, body: first }
}

export function cutThroughFoil(
  polygon: FoilPoint[],
  from: FoilPoint,
  to: FoilPoint
) {
  const dx = to.x - from.x
  const dy = to.y - from.y
  let closest = Infinity
  let exit: FoilEdge | null = null
  for (const [edge, a] of polygon.entries()) {
    const b = polygon[(edge + 1) % polygon.length]
    const hit = foilCrossing(from, to, a, b)
    if (!hit || hit.t <= 0.001 || hit.t >= closest) continue
    closest = hit.t
    exit = {
      p: { x: from.x + dx * hit.t, y: from.y + dy * hit.t },
      edge,
      t: hit.u,
      distance: 0,
    }
  }
  return exit
}
