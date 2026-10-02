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
