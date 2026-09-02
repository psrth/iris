/** iris mark: three concentric quarter-arcs rising from bottom-left to top-right. */
export default function Mark({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3.2" aria-hidden="true">
      <path d="M22 3a19 19 0 0 0-19 19" />
      <path d="M22 10a12 12 0 0 0-12 12" />
      <path d="M22 17a5 5 0 0 0-5 5" />
    </svg>
  );
}
