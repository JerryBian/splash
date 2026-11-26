import type { ReactNode } from 'react'

/**
 * Inline SVG icons, stroke-based, currentColor. Hand-picked subset instead of
 * an icon package: the whole set is a few kilobytes and tree-shaking is not a
 * concern when there is nothing to shake.
 */

type IconProps = {
  size?: number
  className?: string
  strokeWidth?: number
}

function icon(paths: ReactNode) {
  return function Icon({ size = 20, className, strokeWidth = 1.8 }: IconProps) {
    return (
      <svg
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={strokeWidth}
        strokeLinecap="round"
        strokeLinejoin="round"
        className={className}
        aria-hidden="true"
      >
        {paths}
      </svg>
    )
  }
}

export const CheckIcon = icon(<polyline points="20 6 9 17 4 12" />)
export const PlusIcon = icon(<path d="M12 5v14M5 12h14" />)
export const XIcon = icon(<path d="M18 6 6 18M6 6l12 12" />)
export const MinusIcon = icon(<path d="M5 12h14" />)
export const FlameIcon = icon(
  <path d="M12 22c4.4 0 7-2.8 7-6.5 0-3.2-2-5.3-3.5-7C14 6.8 13 5 13 2c-3 2-5 5-5 8-1-.8-1.5-2-1.5-3C5 8.5 5 10.6 5 12c0 5.5 2.6 10 7 10Z" />,
)
export const ChartIcon = icon(<path d="M4 20V10M10 20V4M16 20v-7M22 20H2" />)
export const CalendarIcon = icon(
  <>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M8 3v4M16 3v4M3 10h18" />
  </>,
)
export const SlidersIcon = icon(
  <>
    <path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h10M18 18h2" />
    <circle cx="16" cy="6" r="2" />
    <circle cx="10" cy="12" r="2" />
    <circle cx="16" cy="18" r="2" />
  </>,
)
export const UserIcon = icon(
  <>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21c0-4 3.6-6 8-6s8 2 8 6" />
  </>,
)
export const LockIcon = icon(
  <>
    <rect x="5" y="11" width="14" height="10" rx="2" />
    <path d="M8 11V7a4 4 0 0 1 8 0v4" />
  </>,
)
export const TrashIcon = icon(
  <>
    <path d="M3 6h18M8 6V4h8v2M6 6l1 15h10l1-15" />
    <path d="M10 11v6M14 11v6" />
  </>,
)
export const PencilIcon = icon(
  <path d="M4 20h4L20 8a2.8 2.8 0 0 0-4-4L4 16v4ZM14 6l4 4" />,
)
export const ArchiveIcon = icon(
  <>
    <rect x="3" y="4" width="18" height="5" rx="1" />
    <path d="M5 9v11h14V9M10 13h4" />
  </>,
)
export const ChevronLeftIcon = icon(<polyline points="15 18 9 12 15 6" />)
export const ChevronRightIcon = icon(<polyline points="9 18 15 12 9 6" />)
export const ClockIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 3" />
  </>,
)
export const DownloadIcon = icon(<path d="M12 3v12m0 0 4-4m-4 4-4-4M4 21h16" />)
export const UploadIcon = icon(<path d="M12 15V3m0 0-4 4m4-4 4 4M4 21h16" />)
export const SunIcon = icon(
  <>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </>,
)
export const MoonIcon = icon(<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />)
export const MonitorIcon = icon(
  <>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </>,
)
export const SearchIcon = icon(
  <>
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.5-3.5" />
  </>,
)
export const LogOutIcon = icon(<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />)
export const LogInIcon = icon(<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3" />)
export const EyeIcon = icon(
  <>
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
    <circle cx="12" cy="12" r="3" />
  </>,
)
export const InfoIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 11v5M12 8h.01" />
  </>,
)
export const AlertIcon = icon(
  <path d="M12 3 2 20h20L12 3ZM12 10v4M12 17.5h.01" />,
)
export const RefreshIcon = icon(
  <path d="M21 12a9 9 0 1 1-2.6-6.3M21 3v6h-6" />,
)
export const SkipIcon = icon(<path d="M5 4l10 8-10 8V4ZM19 5v14" />)
export const TargetIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <circle cx="12" cy="12" r="5" />
    <circle cx="12" cy="12" r="1" />
  </>,
)
export const TrophyIcon = icon(
  <path d="M8 21h8M12 17v4M7 4h10v6a5 5 0 0 1-10 0V4ZM7 6H4a3 3 0 0 0 3 5M17 6h3a3 3 0 0 1-3 5" />,
)
export const LayersIcon = icon(
  <path d="m12 2 9 5-9 5-9-5 9-5ZM3 12l9 5 9-5M3 17l9 5 9-5" />,
)
export const PinIcon = icon(
  <>
    <path d="M9 3h6l-1 7 3 3H7l3-3-1-7Z" />
    <path d="M12 13v8" />
  </>,
)
export const CameraIcon = icon(
  <>
    <path d="M4 8h3l2-3h6l2 3h3v11H4Z" />
    <circle cx="12" cy="13" r="3.5" />
  </>,
)

/**
 * The splash mark: an ink blot with flying splatter and a knocked-out check.
 * The wobbly silhouette is the brand — the check-in lands like a drop of ink,
 * which is also what the splatter animation does. Deliberately not a circle
 * or a rounded square; those read as every other check-in app.
 */
export function SealMark({ size = 28, className }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" className={className} aria-hidden="true">
      <path
        d="M28.9 14.6c.7 4.7-1.4 9.6-5.4 12.1-4.1 2.5-9.7 2.4-13.6-.5-3.9-2.9-6.3-8-5.6-12.9C5 8.5 8.6 4.4 13.3 3.2c4.6-1.2 9.9.2 13 3.9 1.6 1.9 2.3 4.6 2.6 7.5Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.1"
      />
      <polyline
        points="10.4 16.1 14.4 20.2 22.1 11.4"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="28.4" cy="4.4" r="1.8" fill="currentColor" />
      <circle cx="30.3" cy="9.3" r="1.05" fill="currentColor" />
      <circle cx="24.3" cy="2.1" r="0.95" fill="currentColor" />
    </svg>
  )
}
