import type { IssueStatus } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";

// ---------------------------------------------------------------------------
// Geometry constants (viewBox 0 0 14 14, center 7,7)
// ---------------------------------------------------------------------------

const CX = 7;
const CY = 7;
const OUTER_R = 6;
const FILL_R = 3.5;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a pie-wedge SVG path from 12 o'clock, clockwise */
function piePath(cx: number, cy: number, r: number, progress: number): string {
  const angle = 2 * Math.PI * progress;
  const endX = cx + r * Math.sin(angle);
  const endY = cy - r * Math.cos(angle);
  const largeArc = progress > 0.5 ? 1 : 0;
  return `M${cx},${cy} L${cx},${cy - r} A${r},${r} 0 ${largeArc},1 ${endX},${endY} Z`;
}

// ---------------------------------------------------------------------------
// Base component — dashed outer ring + pie fill + optional center icon
// ---------------------------------------------------------------------------

function ProgressCircle({
  progress,
  children,
}: {
  progress: number;
  children?: React.ReactNode;
}) {
  return (
    <>
      {/* Outer dashed ring */}
      <circle
        cx={CX}
        cy={CY}
        r={OUTER_R}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeDasharray="3.14 0"
        strokeDashoffset={-0.7}
      />
      {/* Progress fill */}
      {progress === 1 ? (
        <circle cx={CX} cy={CY} r={OUTER_R} fill="currentColor" />
      ) : progress > 0 ? (
        <path d={piePath(CX, CY, FILL_R, progress)} fill="currentColor" />
      ) : null}
      {children}
    </>
  );
}

// ---------------------------------------------------------------------------
// Per-status renderers
// ---------------------------------------------------------------------------

/** 16 small dots arranged in a ring */
function BacklogIcon() {
  const count = 16;
  const dotR = 0.55;
  return (
    <g>
      {Array.from({ length: count }, (_, i) => {
        const angle = (i / count) * Math.PI * 2 - Math.PI / 2;
        return (
          <circle
            key={i}
            cx={CX + OUTER_R * Math.cos(angle)}
            cy={CY + OUTER_R * Math.sin(angle)}
            r={dotR}
            fill="currentColor"
          />
        );
      })}
    </g>
  );
}

function TodoIcon() {
  return <ProgressCircle progress={0} />;
}

function InProgressIcon() {
  return <ProgressCircle progress={0.5} />;
}

function InReviewIcon() {
  return <ProgressCircle progress={0.75} />;
}

function DoneIcon() {
  return (
    <ProgressCircle progress={1}>
      <path
        d="M10.951 4.24896C11.283 4.58091 11.283 5.11909 10.951 5.45104L5.95104 10.451C5.61909 10.783 5.0809 10.783 4.74896 10.451L2.74896 8.45104C2.41701 8.11909 2.41701 7.5809 2.74896 7.24896C3.0809 6.91701 3.61909 6.91701 3.95104 7.24896L5.35 8.64792L9.74896 4.24896C10.0809 3.91701 10.6191 3.91701 10.951 4.24896Z"
        fill="white"
        stroke="none"
      />
    </ProgressCircle>
  );
}

/** Outer ring + prohibition slash (🚫 style) */
function BlockedIcon() {
  return (
    <ProgressCircle progress={0}>
      <line
        x1={CX + FILL_R * Math.cos(Math.PI * 0.75)}
        y1={CY - FILL_R * Math.sin(Math.PI * 0.75)}
        x2={CX + FILL_R * Math.cos(-Math.PI * 0.25)}
        y2={CY - FILL_R * Math.sin(-Math.PI * 0.25)}
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinecap="round"
      />
    </ProgressCircle>
  );
}

/** Filled circle + white X — the terminal-state counterpart to DoneIcon's
 *  filled circle + white checkmark. */
/**
 * The prohibition sign — ring plus one diagonal bar.
 *
 * Cancelled is terminal like Done, but it is not an OUTCOME the way Done is:
 * nothing was produced, the work was called off. Drawing it as Done's twin (a
 * filled disc with a white mark) gave the two the same weight and left the
 * reader to tell them apart by which mark was inside a solid blob at 14px.
 * A ring with a bar through it is read as "stopped" before it is read as a
 * shape, which is the whole point of borrowing a sign everyone already knows.
 *
 * `progress={0}` reuses the same ring every other status draws, so the stroke
 * weight matches the row it sits in rather than being tuned by eye.
 *
 * The bar runs to the ring's centreline (radius x cos45), the proportion the
 * standard sign uses — pulled in any further and it reads as a dash floating
 * inside a circle instead of one continuous stroke.
 */
function CancelledIcon() {
  const reach = OUTER_R * Math.SQRT1_2;
  return (
    <ProgressCircle progress={0}>
      <line
        x1={CX - reach}
        y1={CY - reach}
        x2={CX + reach}
        y2={CY + reach}
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinecap="round"
      />
    </ProgressCircle>
  );
}

// ---------------------------------------------------------------------------
// Renderer map
// ---------------------------------------------------------------------------

const STATUS_RENDERERS: Record<IssueStatus, () => React.ReactNode> = {
  backlog: BacklogIcon,
  todo: TodoIcon,
  in_progress: InProgressIcon,
  in_review: InReviewIcon,
  done: DoneIcon,
  blocked: BlockedIcon,
  cancelled: CancelledIcon,
};

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function StatusIcon({
  status,
  className = "h-4 w-4",
  inheritColor = false,
}: {
  status: IssueStatus | string;
  className?: string;
  inheritColor?: boolean;
}) {
  const knownStatus =
    status in STATUS_RENDERERS ? (status as IssueStatus) : null;
  const cfg = knownStatus ? STATUS_CONFIG[knownStatus] : null;
  const Renderer = knownStatus ? STATUS_RENDERERS[knownStatus] : TodoIcon;

  return (
    <svg
      viewBox="0 0 14 14"
      fill="none"
      className={`${className} ${inheritColor ? "" : (cfg?.iconColor ?? "text-muted-foreground")} shrink-0`}
    >
      <Renderer />
    </svg>
  );
}
