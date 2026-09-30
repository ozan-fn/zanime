import type { ReactElement } from 'react';

interface IconProps {
  name: keyof typeof paths;
  className?: string;
}

const paths: Record<string, ReactElement> = {
  search: <path d="m21 21-4.35-4.35M17 11a6 6 0 1 1-12 0 6 6 0 0 1 12 0Z" />,
  'arrow-left': <path d="M19 12H5m0 0 7 7m-7-7 7-7" />,
  'chevron-left': <path d="m15 18-6-6 6-6" />,
  'chevron-right': <path d="m9 18 6-6-6-6" />,
  'rotate-cw': <path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6" />,
  cc: (
    <>
      <rect x="2" y="5" width="20" height="14" rx="3" />
      <path d="M10 10.5a2 2 0 1 0 0 3" />
      <path d="M17 10.5a2 2 0 1 0 0 3" />
    </>
  ),
  maximize: <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3" />,
  minimize: (
    <path d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3" />
  ),
};

export function Icon({ name, className = 'size-4' }: IconProps) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}
