export default function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="row">
      <span className="label">{label}</span>
      <div className="rowBody">{children}</div>
    </section>
  );
}
