interface PulseDotProps {
  color?: string;
  size?: number;
  className?: string;
}

export default function PulseDot({ color = 'var(--accent)', size = 8, className = '' }: PulseDotProps) {
  return (
    <span
      className={`inline-block rounded-full animate-pulse-glow ${className}`}
      style={{ width: size, height: size, backgroundColor: color, boxShadow: `0 0 6px ${color}` }}
    />
  );
}
