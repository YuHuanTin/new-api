import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type PointerEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks/use-media-query'

import {
  cutThroughFoil,
  foilArea,
  foilCrossing,
  insideFoil,
  nearestFoilEdge,
  SEALED_CARD,
  splitFoil,
  type FoilEdge,
  type FoilPoint,
} from '../lib/checkin-card-tear'
import {
  CARD_VIEW_BOX,
  CheckinCardArtwork,
  CheckinCardPreview,
  type CheckinCardNumber,
} from './checkin-card-art'

type RunningTear = {
  pointerId: number
  previousPoint: FoilPoint
  edge: FoilEdge | null
  path: FoilPoint[]
}

const points = (polygon: FoilPoint[]) =>
  polygon.map(({ x, y }) => `${x},${y}`).join(' ')

export function CheckinCardTear(props: {
  card: CheckinCardNumber
  onTorn: () => void
}) {
  const { t } = useTranslation()
  const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const id = useId()
  const button = useRef<HTMLButtonElement>(null)
  const foil = useRef<SVGSVGElement>(null)
  const active = useRef<RunningTear | null>(null)
  const completed = useRef(false)
  const [{ body, piece }, setFoil] = useState({
    body: SEALED_CARD,
    piece: [] as FoilPoint[],
  })
  const [trace, setTrace] = useState<FoilPoint[]>([])
  const [opened, setOpened] = useState(false)
  const [flipped, setFlipped] = useState(false)
  const canTear = reducedMotion || flipped
  const attachTurn = useCallback((node: HTMLDivElement | null) => {
    if (!node) return
    const finishTurn = (event: AnimationEvent) => {
      if (event.target === node) setFlipped(true)
    }
    node.addEventListener('animationend', finishTurn)
    return () => node.removeEventListener('animationend', finishTurn)
  }, [])

  useEffect(() => {
    if (canTear) button.current?.focus()
  }, [canTear])
  useEffect(() => {
    if (!opened) return
    const timer = window.setTimeout(props.onTorn, reducedMotion ? 0 : 450)
    return () => window.clearTimeout(timer)
  }, [opened, props.onTorn, reducedMotion])

  const pointAt = (event: PointerEvent<HTMLDivElement>) => {
    const bounds = foil.current?.getBoundingClientRect()
    if (!bounds) return
    return {
      x: ((event.clientX - bounds.left) * 100) / Math.max(1, bounds.width),
      y: ((event.clientY - bounds.top) * 145) / Math.max(1, bounds.height),
    }
  }

  const startTearing = (event: PointerEvent<HTMLDivElement>) => {
    if (
      !canTear ||
      event.button !== 0 ||
      completed.current ||
      active.current ||
      button.current?.contains(event.target as Node)
    ) {
      return
    }
    const point = pointAt(event)
    if (!point) return

    const edge = nearestFoilEdge(body, point)
    const startsOnCard = edge.distance === 0 || insideFoil(body, point)
    if (startsOnCard && edge.distance > 22) return
    active.current = {
      pointerId: event.pointerId,
      previousPoint: point,
      edge: startsOnCard ? edge : null,
      path: [],
    }
    event.currentTarget.setPointerCapture?.(event.pointerId)
  }

  const removePiece = (
    edge: FoilEdge,
    path: FoilPoint[],
    exit: FoilEdge,
    forceOpen = false
  ) => {
    const cut = splitFoil(body, edge, path, exit)
    if (foilArea(cut.piece) < 12 && !forceOpen) return
    setFoil(cut)
    let sealRemoved = 0
    for (let i = 0; i < 20; i++) {
      if (!insideFoil(cut.body, { x: 2.5 + 5 * i, y: 3 })) sealRemoved++
    }
    if (forceOpen || sealRemoved >= 7 || foilArea(cut.body) < 14500 * 0.87) {
      completed.current = true
      setOpened(true)
    }
  }

  const followPointer = (event: PointerEvent<HTMLDivElement>) => {
    const tear = active.current
    if (!tear || tear.pointerId !== event.pointerId) return
    const target = pointAt(event)
    if (!target) return
    const edge = tear.edge ?? cutThroughFoil(body, tear.previousPoint, target)
    tear.previousPoint = target
    if (!edge) return
    tear.edge = edge
    let tip = tear.path.at(-1) ?? edge.p
    // A sequence of small, uneven steps makes the edge fray as the pointer moves.
    for (let i = 0; i < 60; i++) {
      const dx = target.x - tip.x
      const dy = target.y - tip.y
      const distance = Math.hypot(dx, dy)
      if (distance < 3) break
      const wobble = Math.sin(tear.path.length * 2.7) * 0.75
      const next = {
        x: tip.x + (dx * 3 - dy * wobble) / distance,
        y: tip.y + (dy * 3 + dx * wobble) / distance,
      }
      if (
        tear.path
          .slice(0, -2)
          .some((p, index) =>
            foilCrossing(tip, next, tear.path[index - 1] ?? edge.p, p)
          )
      ) {
        break
      }
      const exit = cutThroughFoil(body, tip, next)
      if (exit) {
        active.current = null
        setTrace([])
        removePiece(edge, tear.path, exit)
        return
      }
      if (!insideFoil(body, next)) break
      tear.path.push(next)
      tip = next
    }
    setTrace([edge.p, ...tear.path])
  }

  const stopTearing = (event: PointerEvent<HTMLDivElement>) => {
    if (active.current?.pointerId !== event.pointerId) return
    active.current = null
    setTrace([])
  }

  const tearAutomatically = () => {
    if (completed.current) return
    active.current = null
    setTrace([])
    const start = cutThroughFoil(body, { x: -1, y: 46 }, { x: 101, y: 46 })
    const end = cutThroughFoil(body, { x: 101, y: 46 }, { x: -1, y: 46 })
    if (start && end && end.p.x > start.p.x) {
      const line = Array.from({ length: 18 }, (_, i) => ({
        x: start.p.x + ((end.p.x - start.p.x) * (i + 1)) / 19,
        y:
          46 -
          Math.sin(((i + 1) * Math.PI) / 19) * 11 +
          Math.sin(i * 2.2) * 1.6,
      }))
      removePiece(start, line, end, true)
    } else {
      completed.current = true
      setOpened(true)
    }
  }

  const tip = trace.at(-1)
  const edge = trace[0]
  const hinge = tip && nearestFoilEdge(body, tip)
  const flap =
    edge && hinge && trace.length > 3 && hinge.distance > 1
      ? splitFoil(body, nearestFoilEdge(body, edge), trace.slice(1), hinge)
      : null
  const pieceOutline = points(piece)
  const description = t('Drag across the card to tear it open')

  return (
    <div
      className='flex flex-col items-center gap-3'
      onPointerDown={startTearing}
      onPointerMove={followPointer}
      onPointerUp={(event) => {
        followPointer(event)
        stopTearing(event)
      }}
      onPointerCancel={stopTearing}
      onLostPointerCapture={stopTearing}
    >
      {canTear && !opened && (
        <div
          className='absolute inset-0 cursor-grab touch-none select-none active:cursor-grabbing'
          aria-hidden='true'
        />
      )}
      <p className='pointer-events-none text-sm font-medium text-amber-50'>
        {description}
      </p>
      <div className='checkin-wish-card-grid relative my-2 cursor-grab touch-none select-none active:cursor-grabbing'>
        <div className='checkin-wish-card-face checkin-wish-turn'>
          <div ref={attachTurn} className='checkin-wish-turn-body'>
            <div className='checkin-wish-turn-back' aria-hidden='true'>
              <CheckinCardPreview />
            </div>
            <div className='checkin-wish-turn-edge' aria-hidden='true' />
            <div className='checkin-wish-turn-front'>
              <svg
                ref={foil}
                viewBox={CARD_VIEW_BOX}
                preserveAspectRatio='none'
                role='img'
                aria-label={description}
                className='checkin-wish-foil block size-full overflow-visible'
                data-torn={opened}
              >
                <defs>
                  <CheckinCardArtwork id={id} front={props.card} />
                  <linearGradient id={`${id}-silver`}>
                    <stop stopColor='#888ea9' />
                    <stop offset='.45' stopColor='#fff3dc' />
                    <stop offset='1' stopColor='#9299bb' />
                  </linearGradient>
                  <radialGradient id={`${id}-glow`}>
                    <stop stopColor='#fbdfa5' stopOpacity='.76' />
                    <stop offset='1' stopColor='#fbdfa5' stopOpacity='0' />
                  </radialGradient>
                  <clipPath id={`${id}-body`}>
                    <polygon points={points(flap?.body ?? body)} />
                  </clipPath>
                  <clipPath id={`${id}-piece`}>
                    <polygon points={pieceOutline} />
                  </clipPath>
                </defs>
                <ellipse
                  cx='50'
                  cy='49'
                  rx='70'
                  ry='64'
                  fill={`url(#${id}-glow)`}
                  className='checkin-wish-foil-glow'
                />
                <use href={`#${id}-art`} clipPath={`url(#${id}-body)`} />
                {flap && tip && (
                  <polygon
                    points={points(flap.piece)}
                    fill={`url(#${id}-silver)`}
                    stroke='#fff0c8'
                    strokeWidth='.45'
                    transform={`translate(${tip.x} ${tip.y}) rotate(${Math.min(15, trace.length / 2)}) scale(1 .86) translate(${-tip.x} ${-tip.y})`}
                  />
                )}
                {trace.length > 1 && (
                  <polyline
                    points={points(trace)}
                    stroke='#fff0c8'
                    strokeWidth='1.1'
                    fill='none'
                  />
                )}
                {piece.length > 0 && (
                  <g
                    key={pieceOutline}
                    className='checkin-wish-foil-piece'
                    clipPath={`url(#${id}-piece)`}
                  >
                    <use href={`#${id}-art`} />
                    <polygon
                      points={pieceOutline}
                      fill='none'
                      stroke='#fff3d3'
                      strokeWidth='1.2'
                    />
                  </g>
                )}
              </svg>
            </div>
          </div>
        </div>
      </div>
      <Button
        ref={button}
        disabled={!canTear || opened}
        onClick={tearAutomatically}
        className='relative bg-amber-100 text-[#302317] hover:bg-amber-50 focus-visible:ring-amber-200'
      >
        {t('Tear open card')}
      </Button>
    </div>
  )
}
