
import { useId } from 'react'

import checkinAstrolabe from '@/assets/checkin-astrolabe.jpg'
import checkinMihoPines from '@/assets/checkin-miho-pines.jpg'
import checkinMoonGeese from '@/assets/checkin-moon-geese.jpg'

export const CARD_VIEW_BOX = '0 0 100 145'

export type CheckinCardNumber = 1 | 2 | 3

// The Met Open Access (CC0).
const cardFronts = {
  // Regiomontanus, Astrolabe (1462): https://www.metmuseum.org/art/collection/search/923639
  1: {
    href: checkinAstrolabe,
    x: -17.8,
    y: -12.9,
    width: 138.2,
    height: 146.8,
  },
  // Hiroshige, Wild Geese Flying under the Full Moon: https://www.metmuseum.org/art/collection/search/36742
  2: { href: checkinMoonGeese, x: 5, y: -52, width: 90, height: 259 },
  // Hiroshige, Pine Groves of Miho in Suruga Province: https://www.metmuseum.org/art/collection/search/56929
  3: { href: checkinMihoPines, x: 5, y: 6, width: 90, height: 133 },
}

export function CheckinCardArtwork(props: {
  id: string
  front?: CheckinCardNumber
}) {
  const { id, front } = props
  const art = front ? cardFronts[front] : undefined
  const isDial = front === 1
  return (
    <>
      <linearGradient id={`${id}-rim`} x1='0' y1='0' x2='1' y2='1'>
        <stop stopColor='#fff0b7' />
        <stop offset='.28' stopColor='#986737' />
        <stop offset='.55' stopColor='#e8bc77' />
        <stop offset='.76' stopColor='#614328' />
        <stop offset='1' stopColor='#f9e2a5' />
      </linearGradient>
      <radialGradient id={`${id}-field`} cx='.42' cy='.37' r='.78'>
        <stop stopColor={front ? '#3e6373' : '#253e59'} />
        <stop offset='.67' stopColor={front ? '#173141' : '#15273f'} />
        <stop offset='1' stopColor='#090f21' />
      </radialGradient>
      <linearGradient id={`${id}-gold`} x1='0' y1='0' x2='.85' y2='1'>
        <stop stopColor='#ffedb7' />
        <stop offset='.29' stopColor='#c38b45' />
        <stop offset='.49' stopColor='#ffe4a0' />
        <stop offset='1' stopColor='#805331' />
      </linearGradient>
      <linearGradient id={`${id}-shine`} x1='0' y1='0' x2='1' y2='1'>
        <stop stopColor='#fff8d5' stopOpacity='.2' />
        <stop offset='.47' stopColor='#fff8d5' stopOpacity='0' />
        <stop offset='.78' stopColor='#c7e3ed' stopOpacity='.11' />
        <stop offset='1' stopColor='#c7e3ed' stopOpacity='0' />
      </linearGradient>
      <pattern
        id={`${id}-etch`}
        width='8'
        height='8'
        patternUnits='userSpaceOnUse'
      >
        <path
          d='M-2 2 2-2M0 8l8-8M6 10l4-4'
          fill='none'
          stroke='#e6c48e'
          strokeOpacity='.1'
          strokeWidth='.35'
        />
      </pattern>
      {art && (
        <>
          <rect id={`${id}-frame`} x='8' y='8' width='84' height='129' rx='2' />
          <clipPath id={`${id}-picture`}>
            {isDial ? (
              <circle cx='50' cy='73' r='37.5' />
            ) : (
              <use href={`#${id}-frame`} />
            )}
          </clipPath>
        </>
      )}
      <g id={`${id}-art`}>
        <rect
          x='.4'
          y='.4'
          width='99.2'
          height='144.2'
          rx='5.5'
          fill={`url(#${id}-rim)`}
        />
        <rect
          x='2.2'
          y='2.2'
          width='95.6'
          height='140.6'
          rx='4.2'
          fill={`url(#${id}-field)`}
        />
        <rect
          x='3'
          y='3'
          width='94'
          height='139'
          rx='4'
          fill={`url(#${id}-etch)`}
        />
        <path
          d='M6 22V6h16m56 0h16v16m0 101v16H78m-56 0H6v-16'
          fill='none'
          stroke='#f8dba0'
          strokeWidth='.6'
        />
        <path
          d='M11 25V11h14m50 0h14v14m0 95v14H75m-50 0H11v-14'
          fill='none'
          stroke='#cc9b64'
          strokeWidth='.45'
        />
        <path
          d='M50 13l3 4-3 4-3-4zm0 111 3 4-3 4-3-4z'
          fill={`url(#${id}-gold)`}
        />
        {art ? (
          <>
            {isDial && (
              <>
                <path
                  d='M15 119V53a35 35 0 0 1 70 0v66Z'
                  fill='#0a1424'
                  fillOpacity='.45'
                  stroke='#d5ad75'
                  strokeWidth='.35'
                />
                <circle cx='50' cy='73' r='39.8' fill={`url(#${id}-gold)`} />
                <circle cx='50' cy='73' r='37.5' fill='#ad884c' />
              </>
            )}
            <image
              {...art}
              preserveAspectRatio='none'
              clipPath={`url(#${id}-picture)`}
            />
            {isDial ? (
              <circle
                cx='50'
                cy='73'
                r='37.8'
                fill='none'
                stroke='#2f2117'
                strokeWidth='.65'
              />
            ) : (
              <use
                href={`#${id}-frame`}
                fill='none'
                stroke={`url(#${id}-gold)`}
                strokeWidth='.7'
              />
            )}
          </>
        ) : (
          <>
            <path
              d='M50 24 88 72.5 50 121 12 72.5Z'
              fill='#789abe'
              fillOpacity='.09'
              stroke='#dcbc87'
              strokeWidth='.75'
            />
            <path
              d='M50 32 80 72.5 50 113 20 72.5Z'
              fill='none'
              stroke='#dcbc87'
              strokeOpacity='.5'
              strokeWidth='.45'
            />
            <path
              d='M20 72.5q30-32 60 0-30 32-60 0Zm30-31q28 31 0 62-28-31 0-62Z'
              fill='none'
              stroke='#a9cadc'
              strokeOpacity='.58'
              strokeWidth='.55'
            />
            <circle
              cx='50'
              cy='72.5'
              r='15'
              fill='#112136'
              stroke='#dcbc87'
              strokeWidth='.55'
            />
            <path
              d='M50 61 53 69.5 61 72.5 53 75.5 50 84 47 75.5 39 72.5 47 69.5Z'
              fill={`url(#${id}-gold)`}
            />
          </>
        )}
        <rect
          x='2.5'
          y='2.5'
          width='95'
          height='140'
          rx='4'
          fill={`url(#${id}-shine)`}
        />
      </g>
    </>
  )
}

export function CheckinCardPreview() {
  const id = useId()
  return (
    <svg
      viewBox={CARD_VIEW_BOX}
      preserveAspectRatio='none'
      aria-hidden='true'
      className='block size-full'
    >
      <defs>
        <CheckinCardArtwork id={id} />
      </defs>
      <use href={`#${id}-art`} />
    </svg>
  )
}
