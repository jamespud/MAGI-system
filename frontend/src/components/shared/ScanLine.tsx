interface ScanLineProps {
  className?: string;
  height?: number;
}

export default function ScanLine({ className = '', height = 2 }: ScanLineProps) {
  return (
    <div
      className={`pointer-events-none absolute inset-x-0 animate-scanline ${className}`}
      style={{
        height: `${height}px`,
        background: 'linear-gradient(180deg, transparent 0%, rgba(74, 158, 255, 0.06) 50%, transparent 100%)',
      }}
    />
  );
}
