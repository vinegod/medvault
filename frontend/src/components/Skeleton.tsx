interface Props {
  width?: string | number;
  height?: string | number;
  count?: number;
}

const barStyle = (width: string | number, height: string | number): React.CSSProperties => ({
  width,
  height,
  borderRadius: 8,
  background:
    "linear-gradient(90deg, var(--color-surface) 25%, var(--color-border) 50%, var(--color-surface) 75%)",
  backgroundSize: "200% 100%",
  animation: "skeleton-shimmer 1.4s ease-in-out infinite",
});

export function Skeleton({ width = "100%", height = 88, count = 1 }: Props) {
  if (count === 1) return <div style={barStyle(width, height)} />;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
      {Array.from({ length: count }, (_, i) => (
        <div key={i} style={barStyle(width, height)} />
      ))}
    </div>
  );
}
