import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { ProbeSeriesPoint } from '../types/yals';

interface LatencyChartProps {
  points: ProbeSeriesPoint[];
  // Used to derive a stable per-target color, so each target keeps its own hue
  // across refreshes instead of flickering.
  name: string;
}

function fmtTime(ts: number): string {
  const d = new Date(ts * 1000);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

function fmtMs(v: number, range: number): string {
  return range < 10 ? v.toFixed(1) : Math.round(v).toString();
}

// hueFromString hashes a name into a 0–359 hue, giving each target a distinct
// but stable color.
function hueFromString(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i += 1) {
    h = (h * 31 + s.charCodeAt(i)) >>> 0;
  }
  return h % 360;
}

// LatencyChart renders a probe target's latency over time as a hand-drawn inline
// SVG (no chart library), with a latency scale on the left (ms) and a time scale
// along the bottom. The line is drawn over a gradient-filled area (opaque near
// the line, fading toward the baseline), tinted with the target's own color.
// Lost cycles (recv === 0) break the area/line and are marked in red. The SVG is
// sized to the measured container width so axis labels are not distorted.
export function LatencyChart({ points, name }: LatencyChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const gradId = useId();

  const hue = useMemo(() => hueFromString(name), [name]);
  const lineColor = `hsl(${hue} 70% 42%)`;
  const fillColor = `hsl(${hue} 75% 50%)`;

  // On small screens shrink the chart (height, margins, tick count) so the
  // expanded row stays compact. The container itself can be wider than the
  // viewport (the table scrolls horizontally), so detect the small screen via a
  // viewport media query rather than the measured container width.
  const [compact, setCompact] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 640px)');
    const update = () => setCompact(mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  }, []);

  const HEIGHT = compact ? 120 : 180;
  const M_TOP = compact ? 8 : 10;
  const M_RIGHT = compact ? 8 : 12;
  const M_BOTTOM = compact ? 18 : 26; // room for the time (x) axis labels
  const M_LEFT = compact ? 38 : 52;   // room for the latency (y) axis labels
  const X_LABEL_DY = compact ? 11 : 16;
  const Y_TICKS = compact ? 3 : 4;
  const X_TICKS = compact ? 3 : 4;

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
    // Float both bounds by 5% (top above the max, bottom below the min) so the
    // line never touches the top or bottom edge of the chart.
    const yMax = latMax > 0 ? latMax * 1.05 : 1;
    const yMin = latMin * 0.95;
    const ySpan = yMax - yMin || 1;
    const tsSpan = tsMax - tsMin || 1;

    const xOf = (ts: number) => plotL + ((ts - tsMin) / tsSpan) * plotW;
    const yOf = (lat: number) => plotT + (1 - (lat - yMin) / ySpan) * plotH;

    // Build contiguous segments (split at lost cycles). Each yields a line
    // polyline and a closed area path down to the baseline.
    const segments: { line: string; area: string }[] = [];
    let cur: { x: number; y: number }[] = [];
    const flush = () => {
      if (cur.length === 0) return;
      const line = cur.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
      const first = cur[0];
      const last = cur[cur.length - 1];
      const area = `M ${first.x.toFixed(1)},${plotB.toFixed(1)} L ${cur
        .map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`)
        .join(' L ')} L ${last.x.toFixed(1)},${plotB.toFixed(1)} Z`;
      segments.push({ line, area });
      cur = [];
    };
    for (const p of points) {
      if (p.recv > 0) {
        cur.push({ x: xOf(p.ts), y: yOf(p.latency_ms) });
      } else {
        flush();
      }
    }
    flush();

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
  }, [stats, width, points, HEIGHT, M_TOP, M_RIGHT, M_BOTTOM, M_LEFT, Y_TICKS, X_TICKS]);

  return (
    <div className={`latency-chart${compact ? ' compact' : ''}`} ref={containerRef}>
      {points.length === 0 ? (
        <div className="latency-chart-empty">No data</div>
      ) : !stats ? (
        <div className="latency-chart-empty">No successful samples ({points.length} cycles, all lost)</div>
      ) : chart ? (
        <svg className="latency-chart-svg" width={width} height={HEIGHT} role="img" aria-label="Latency over time">
          <defs>
            <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={fillColor} stopOpacity={0.85} />
              <stop offset="100%" stopColor={fillColor} stopOpacity={0.08} />
            </linearGradient>
          </defs>

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
              <text x={t.x} y={chart.plotB + X_LABEL_DY} textAnchor="middle" className="latency-chart-axis-label">
                {fmtTime(t.ts)}
              </text>
            </g>
          ))}

          {/* gradient-filled area under the line */}
          {chart.segments.map((s, i) => (
            <path key={`area${i}`} d={s.area} fill={`url(#${gradId})`} stroke="none" />
          ))}

          {/* axes (drawn over the area's bottom edge) */}
          <line x1={chart.plotL} y1={chart.plotT} x2={chart.plotL} y2={chart.plotB} stroke="#e5e7eb" strokeWidth={1} />
          <line x1={chart.plotL} y1={chart.plotB} x2={chart.plotR} y2={chart.plotB} stroke="#e5e7eb" strokeWidth={1} />

          {/* packet-loss markers: a faint red band at each lost cycle's time, plus
              a solid red tick on the time axis. */}
          {chart.lossXs.map((x, i) => (
            <line key={`loss${i}`} x1={x} y1={chart.plotT} x2={x} y2={chart.plotB} stroke="#fca5a5" strokeWidth={1} />
          ))}
          {chart.lossXs.map((x, i) => (
            <circle key={`lossd${i}`} cx={x} cy={chart.plotB} r={2.5} fill="#ef4444" />
          ))}

          {/* latency line */}
          {chart.segments.map((s, i) => (
            <polyline key={`line${i}`} points={s.line} fill="none" stroke={lineColor} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
          ))}

          {/* point dots when sparse, so a single / few samples are visible */}
          {stats.valid.length <= 50 && stats.valid.map((p, i) => (
            <circle key={`d${i}`} cx={chart.xOf(p.ts)} cy={chart.yOf(p.latency_ms)} r={2} fill={lineColor} />
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
