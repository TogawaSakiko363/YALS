import { ProbeSeriesPoint } from '../types/yals';

interface LatencyChartProps {
  points: ProbeSeriesPoint[];
}

const VIEW_W = 1000;
const VIEW_H = 140;
const PAD_X = 6;
const PAD_TOP = 10;
const PAD_BOTTOM = 10;

// LatencyChart renders a probe target's latency over time as a hand-drawn inline
// SVG line (no chart library). Lost cycles (recv === 0) break the line so a gap
// is visible rather than a straight bridge. preserveAspectRatio="none" lets it
// fill the row width; vector-effect keeps the stroke crisp despite the scaling.
export function LatencyChart({ points }: LatencyChartProps) {
  if (points.length === 0) {
    return <div className="latency-chart-empty">No data</div>;
  }

  const valid = points.filter((p) => p.recv > 0);
  const lossCount = points.length - valid.length;

  if (valid.length === 0) {
    return <div className="latency-chart-empty">No successful samples ({points.length} cycles, all lost)</div>;
  }

  const lats = valid.map((p) => p.latency_ms);
  const latMin = Math.min(...lats);
  const latMax = Math.max(...lats);
  const latAvg = lats.reduce((a, b) => a + b, 0) / lats.length;

  const tsMin = points[0].ts;
  const tsMax = points[points.length - 1].ts;
  const tsSpan = tsMax - tsMin || 1;
  const latSpan = latMax - latMin || 1;

  const xOf = (ts: number) => PAD_X + ((ts - tsMin) / tsSpan) * (VIEW_W - 2 * PAD_X);
  const yOf = (lat: number) => PAD_TOP + (1 - (lat - latMin) / latSpan) * (VIEW_H - PAD_TOP - PAD_BOTTOM);

  // Split the polyline at lost cycles so the line breaks across gaps.
  const segments: string[] = [];
  let current: string[] = [];
  for (const p of points) {
    if (p.recv > 0) {
      current.push(`${xOf(p.ts).toFixed(1)},${yOf(p.latency_ms).toFixed(1)}`);
    } else if (current.length > 0) {
      segments.push(current.join(' '));
      current = [];
    }
  }
  if (current.length > 0) segments.push(current.join(' '));

  return (
    <div className="latency-chart">
      <svg className="latency-chart-svg" viewBox={`0 0 ${VIEW_W} ${VIEW_H}`} preserveAspectRatio="none" role="img" aria-label="Latency over time">
        <line
          x1={PAD_X}
          y1={VIEW_H - PAD_BOTTOM}
          x2={VIEW_W - PAD_X}
          y2={VIEW_H - PAD_BOTTOM}
          stroke="#e5e7eb"
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
        />
        {segments.map((pts, i) => (
          <polyline
            key={i}
            points={pts}
            fill="none"
            stroke="#111827"
            strokeWidth={1.5}
            strokeLinejoin="round"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </svg>
      <div className="latency-chart-caption">
        <span>min {latMin.toFixed(1)} ms</span>
        <span>avg {latAvg.toFixed(1)} ms</span>
        <span>max {latMax.toFixed(1)} ms</span>
        <span>{valid.length} samples{lossCount > 0 ? ` · ${lossCount} lost` : ''}</span>
      </div>
    </div>
  );
}
