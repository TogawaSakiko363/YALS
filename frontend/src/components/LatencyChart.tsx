import { useEffect, useMemo, useRef, useState } from 'react';
import { ProbeSeriesPoint } from '../types/yals';

interface LatencyChartProps {
  points: ProbeSeriesPoint[];
}

const HEIGHT = 180;
const M_TOP = 10;
const M_RIGHT = 12;
const M_BOTTOM = 26; // room for the time (x) axis labels
const M_LEFT = 52;   // room for the latency (y) axis labels
const Y_TICKS = 4;
const X_TICKS = 4;

function fmtTime(ts: number): string {
  const d = new Date(ts * 1000);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

function fmtMs(v: number, range: number): string {
  return range < 10 ? v.toFixed(1) : Math.round(v).toString();
}

// LatencyChart renders a probe target's latency over time as a hand-drawn inline
// SVG (no chart library), with a latency scale on the left (ms) and a time scale
// along the bottom. Lost cycles (recv === 0) break the line so a gap is visible
// rather than a straight bridge. The SVG is sized to the measured container width
// (real pixel coordinates) so the axis labels are not distorted.
export function LatencyChart({ points }: LatencyChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const update = () => setWidth(el.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const stats = useMemo(() => {
    const valid = points.filter((p) => p.recv > 0);
    if (valid.length === 0) return null;
    const lats = valid.map((p) => p.latency_ms);
    const latMin = Math.min(...lats);
    const latMax = Math.max(...lats);
    return {
      valid,
      lossCount: points.length - valid.length,
      latMin,
      latMax,
      latAvg: lats.reduce((a, b) => a + b, 0) / lats.length,
      tsMin: points[0].ts,
      tsMax: points[points.length - 1].ts
    };
  }, [points]);

  const chart = useMemo(() => {
    if (!stats || width <= 0) return null;

    const plotL = M_LEFT;
    const plotR = width - M_RIGHT;
    const plotT = M_TOP;
    const plotB = HEIGHT - M_BOTTOM;
    const plotW = Math.max(plotR - plotL, 1);
    const plotH = Math.max(plotB - plotT, 1);

    const { latMin, latMax, tsMin, tsMax } = stats;
    // 20% headroom above the peak so the highest latency never touches the top
    // of the chart (e.g. peak 100ms → top of scale 120ms).
    const headroom = latMax > 0 ? latMax * 0.2 : 1;
    const yMin = latMin;
    const yMax = latMax + headroom;
    const ySpan = yMax - yMin || 1;
    const tsSpan = tsMax - tsMin || 1;

    const xOf = (ts: number) => plotL + ((ts - tsMin) / tsSpan) * plotW;
    const yOf = (lat: number) => plotT + (1 - (lat - yMin) / ySpan) * plotH;

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

    const yTicks = Array.from({ length: Y_TICKS }, (_, i) => {
      const v = yMin + (ySpan * i) / (Y_TICKS - 1);
      return { v, y: yOf(v) };
    });
    const xTicks = Array.from({ length: X_TICKS }, (_, i) => {
      const ts = tsMin + (tsSpan * i) / (X_TICKS - 1);
      return { ts, x: xOf(ts) };
    });

    // X positions of lost cycles (recv === 0), for the packet-loss markers.
    const lossXs = points.filter((p) => p.recv === 0).map((p) => xOf(p.ts));

    return { plotL, plotR, plotT, plotB, segments, yTicks, xTicks, ySpan, lossXs, xOf, yOf };
  }, [stats, width, points]);

  return (
    <div className="latency-chart" ref={containerRef}>
      {points.length === 0 ? (
        <div className="latency-chart-empty">No data</div>
      ) : !stats ? (
        <div className="latency-chart-empty">No successful samples ({points.length} cycles, all lost)</div>
      ) : chart ? (
        <svg className="latency-chart-svg" width={width} height={HEIGHT} role="img" aria-label="Latency over time">
          {/* horizontal gridlines + latency (y) axis labels */}
          {chart.yTicks.map((t, i) => (
            <g key={`y${i}`}>
              <line x1={chart.plotL} y1={t.y} x2={chart.plotR} y2={t.y} stroke="#f3f4f6" strokeWidth={1} />
              <text x={chart.plotL - 6} y={t.y} textAnchor="end" dominantBaseline="middle" className="latency-chart-axis-label">
                {fmtMs(t.v, chart.ySpan)}
              </text>
            </g>
          ))}

          {/* time (x) axis labels + ticks */}
          {chart.xTicks.map((t, i) => (
            <g key={`x${i}`}>
              <line x1={t.x} y1={chart.plotB} x2={t.x} y2={chart.plotB + 4} stroke="#d1d5db" strokeWidth={1} />
              <text x={t.x} y={chart.plotB + 16} textAnchor="middle" className="latency-chart-axis-label">
                {fmtTime(t.ts)}
              </text>
            </g>
          ))}

          {/* packet-loss markers: a faint red band at each lost cycle's time, plus
              a solid red tick on the time axis. Drawn under the latency line. */}
          {chart.lossXs.map((x, i) => (
            <line key={`loss${i}`} x1={x} y1={chart.plotT} x2={x} y2={chart.plotB} stroke="#fca5a5" strokeWidth={1} />
          ))}
          {chart.lossXs.map((x, i) => (
            <circle key={`lossd${i}`} cx={x} cy={chart.plotB} r={2.5} fill="#ef4444" />
          ))}

          {/* axes */}
          <line x1={chart.plotL} y1={chart.plotT} x2={chart.plotL} y2={chart.plotB} stroke="#e5e7eb" strokeWidth={1} />
          <line x1={chart.plotL} y1={chart.plotB} x2={chart.plotR} y2={chart.plotB} stroke="#e5e7eb" strokeWidth={1} />

          {/* latency line */}
          {chart.segments.map((pts, i) => (
            <polyline key={i} points={pts} fill="none" stroke="#111827" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
          ))}

          {/* point dots when sparse, so a single / few samples are visible */}
          {stats.valid.length <= 50 && stats.valid.map((p, i) => (
            <circle key={`d${i}`} cx={chart.xOf(p.ts)} cy={chart.yOf(p.latency_ms)} r={2} fill="#111827" />
          ))}
        </svg>
      ) : null}

      {stats && (
        <div className="latency-chart-caption">
          <span>min {stats.latMin.toFixed(1)} ms</span>
          <span>avg {stats.latAvg.toFixed(1)} ms</span>
          <span>max {stats.latMax.toFixed(1)} ms</span>
          <span>{stats.valid.length} samples</span>
          {stats.lossCount > 0 && <span className="latency-chart-loss">● {stats.lossCount} lost</span>}
        </div>
      )}
    </div>
  );
}
