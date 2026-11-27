// Small line icons drawn for this app (original artwork, styled to sit well next to
// the macOS system font). All take the current text colour.
import type { SVGProps } from "react";

type P = SVGProps<SVGSVGElement> & { size?: number };

function Svg({ size = 16, children, ...rest }: P) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.4}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
    >
      {children}
    </svg>
  );
}

export const PlusIcon = (p: P) => (
  <Svg {...p}>
    <path d="M8 3v10M3 8h10" />
  </Svg>
);

export const CloseIcon = (p: P) => (
  <Svg {...p}>
    <path d="M4.5 4.5l7 7M11.5 4.5l-7 7" />
  </Svg>
);

export const TerminalIcon = (p: P) => (
  <Svg {...p}>
    <rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2.5" />
    <path d="M4.75 6.25L6.75 8l-2 1.75M8.5 10h2.75" />
  </Svg>
);

export const ShieldIcon = (p: P) => (
  <Svg {...p}>
    <path d="M8 1.75l5 1.9v4.1c0 3-2.1 5.4-5 6.5-2.9-1.1-5-3.5-5-6.5v-4.1l5-1.9z" />
    <path d="M6.25 7.25V6.4a1.75 1.75 0 013.5 0v.85M5.75 7.25h4.5v3h-4.5z" />
  </Svg>
);

export const SparkIcon = (p: P) => (
  <Svg {...p}>
    <path d="M8 2.25c.35 2.6 1.15 3.4 3.75 3.75C9.15 6.35 8.35 7.15 8 9.75 7.65 7.15 6.85 6.35 4.25 6 6.85 5.65 7.65 4.85 8 2.25z" />
    <path d="M12 9.5c.18 1.2.55 1.57 1.75 1.75-1.2.18-1.57.55-1.75 1.75-.18-1.2-.55-1.57-1.75-1.75 1.2-.18 1.57-.55 1.75-1.75z" />
  </Svg>
);

export const HandIcon = (p: P) => (
  <Svg {...p}>
    <path d="M5.5 8V3.6a.9.9 0 011.8 0V7.5M7.3 7V2.9a.9.9 0 011.8 0V7M9.1 7.2V3.7a.9.9 0 011.8 0v4.8" />
    <path d="M10.9 7.6l.6-1a.9.9 0 011.55.9l-1.6 3.4A3.9 3.9 0 017.9 13.5h-.3A3.6 3.6 0 014 9.9V6.7a.8.8 0 011.5-.4" />
  </Svg>
);

export const KeyIcon = (p: P) => (
  <Svg {...p}>
    <circle cx="5.5" cy="10.5" r="3" />
    <path d="M7.6 8.4L13 3M11 5l1.5 1.5M9.6 6.4l1.2 1.2" />
  </Svg>
);

export const InfoIcon = (p: P) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="6.25" />
    <path d="M8 7.25V11M8 5h.01" />
  </Svg>
);

export const ArrowUturnIcon = (p: P) => (
  <Svg {...p}>
    <path d="M6 4.5L3 7.5l3 3" />
    <path d="M3.25 7.5h6.5a3.25 3.25 0 010 6.5H8" />
  </Svg>
);
