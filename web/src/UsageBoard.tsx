import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { api, Board, Burn, Subject } from "./api";
import { fmtNum, personColor, agoFrom } from "./format";

type Metric = "weighted" | "cost";
type Window = "5h" | "day" | "week" | "month";

const val = (s: { weighted: number; costUsd: number }, m: Metric) => (m === "cost" ? s.costUsd : s.weighted);
const fmt = (v: number, m: Metric) => (m === "cost" ? "$" + (v || 0).toFixed(2) : fmtNum(v || 0));

// A slice of a bar: one person's share, in their colour.
interface Slice { id: string; name: string; color: string; value: number }
interface ModelRow { label: string; total: number; slices: Slice[] }

// buildModelRows turns per-person usage into one bar per model, sliced by person
// — the "who used this model" view, colour per person.
function buildModelRows(people: Subject[], metric: Metric): ModelRow[] {
  const idx = new Map(people.map((p, i) => [p.id, i] as const));
  const names = new Map(people.map((p) => [p.id, p.name] as const));
  const fam = new Map<string, Map<string, number>>();
  for (const p of people) {
    for (const m of p.byModel) {
      const v = metric === "cost" ? m.costUsd : m.weighted;
      if (v <= 0) continue;
      const label = modelLabel(m.model);
      if (!fam.has(label)) fam.set(label, new Map());
      const pm = fam.get(label)!;
      pm.set(p.id, (pm.get(p.id) || 0) + v);
    }
  }
  const rows: ModelRow[] = [];
  for (const [label, pm] of fam) {
    let total = 0;
    const slices: Slice[] = [];
    for (const [pid, v] of pm) {
      total += v;
      slices.push({ id: pid, name: names.get(pid) || pid, color: personColor(idx.get(pid) ?? 0), value: v });
    }
    slices.sort((a, b) => b.value - a.value);
    rows.push({ label, total, slices });
  }
  return rows.sort((a, b) => b.total - a.total);
}

function modelLabel(m: string): string {
  const lm = (m || "").toLowerCase();
  if (/fable|mythos/.test(lm)) return "Fable / Mythos";
  if (/opus/.test(lm)) return "Opus";
  if (/sonnet/.test(lm)) return "Sonnet";
  if (/haiku/.test(lm)) return "Haiku";
  if (/unknown/.test(lm)) return "Unknown";
  return m || "?";
}

// --- controls --------------------------------------------------------------

function Seg<T extends string>({ value: v, onChange, options }: { value: T; onChange: (v: T) => void; options: [T, string][] }) {
  return (
    <div className="inline-flex rounded-xl border border-line bg-sunken p-1">
      {options.map(([opt, label]) => (
        <button
          key={opt}
          onClick={() => onChange(opt)}
          className="relative rounded-lg px-3.5 py-1.5 text-[14px] font-medium transition-colors"
        >
          {opt === v && (
            <motion.span layoutId="seg-active" className="absolute inset-0 rounded-lg bg-raised-2" transition={{ type: "spring", stiffness: 500, damping: 38 }} />
          )}
          <span className={`relative ${opt === v ? "text-ink" : "text-muted hover:text-ink"}`}>{label}</span>
        </button>
      ))}
    </div>
  );
}

// --- tooltip ---------------------------------------------------------------

interface Tip { x: number; y: number; slice: Slice; rowLabel: string; rowTotal: number; metric: Metric }

function Tooltip({ tip }: { tip: Tip }) {
  const share = tip.rowTotal > 0 ? tip.slice.value / tip.rowTotal : 0;
  return (
    <motion.div
      initial={{ opacity: 0, y: 4, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, scale: 0.97 }}
      transition={{ duration: 0.12 }}
      className="pointer-events-none fixed z-50 w-max max-w-[240px] rounded-xl border border-line bg-raised px-3.5 py-2.5 shadow-2xl"
      style={{ left: Math.min(tip.x + 14, window.innerWidth - 250), top: tip.y + 16 }}
    >
      <div className="flex items-center gap-2 text-[15px] font-semibold">
        <span className="h-2.5 w-2.5 rounded-full" style={{ background: tip.slice.color }} />
        {tip.slice.name}
      </div>
      <div className="mt-1 text-[13.5px] text-muted">
        <b className="text-ink">{fmt(tip.slice.value, tip.metric)}</b> on {tip.rowLabel}
      </div>
      <div className="text-[13px] text-faint">{Math.round(share * 100)}% of all {tip.rowLabel} this window</div>
    </motion.div>
  );
}

// --- bars ------------------------------------------------------------------

function ModelBar({ row, max, metric, onHover }: { row: ModelRow; max: number; metric: Metric; onHover: (t: Omit<Tip, "x" | "y"> | null, e?: React.MouseEvent) => void }) {
  return (
    <div className="grid grid-cols-[92px_1fr_96px] items-center gap-4 py-2.5">
      <div className="truncate text-[15px] font-medium text-ink">{row.label}</div>
      <div className="relative flex h-8 overflow-hidden rounded-lg bg-sunken ring-1 ring-inset ring-line/60">
        <motion.div className="flex h-full" initial={{ width: 0 }} animate={{ width: `${(row.total / max) * 100}%` }} transition={{ duration: 0.55, ease: [0.2, 0.8, 0.2, 1] }}>
          {row.slices.map((s) => (
            <motion.span
              key={s.id}
              className="h-full cursor-pointer border-r border-ground/40 last:border-0 transition-[filter] hover:brightness-125"
              style={{ width: `${(s.value / row.total) * 100}%`, background: s.color }}
              onMouseEnter={(e) => onHover({ slice: s, rowLabel: row.label, rowTotal: row.total, metric }, e)}
              onMouseMove={(e) => onHover({ slice: s, rowLabel: row.label, rowTotal: row.total, metric }, e)}
              onMouseLeave={() => onHover(null)}
              whileHover={{ filter: "brightness(1.2)" }}
            />
          ))}
        </motion.div>
      </div>
      <div className="text-right text-[15px] font-semibold tabular-nums text-ink">{fmt(row.total, metric)}</div>
    </div>
  );
}

function Sparkline({ buckets, metric }: { buckets: Burn["buckets"]; metric: Metric }) {
  if (buckets.length < 2) return null;
  const vals = buckets.map((b) => (metric === "cost" ? b.costUsd : b.weighted));
  const max = Math.max(...vals, 1), n = vals.length, W = 100, H = 32;
  const line = vals.map((v, i) => `${((i / (n - 1)) * W).toFixed(2)},${(H - (v / max) * H).toFixed(2)}`).join(" ");
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block h-full w-full">
      <defs>
        <linearGradient id="spark" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#6E8BFF" stopOpacity="0.35" />
          <stop offset="100%" stopColor="#6E8BFF" stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon points={`0,${H} ${line} ${W},${H}`} fill="url(#spark)" />
      <motion.polyline points={line} fill="none" stroke="#6E8BFF" strokeWidth={1.6} vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" initial={{ pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 0.8 }} />
    </svg>
  );
}

// --- board -----------------------------------------------------------------

const REFRESH_MS = 12000;

export function UsageBoard() {
  const [win, setWin] = useState<Window>("week");
  const [metric, setMetric] = useState<Metric>("weighted");
  const [people, setPeople] = useState<Subject[]>([]);
  const [burn, setBurn] = useState<Burn["buckets"]>([]);
  const [asOf, setAsOf] = useState("");
  const [err, setErr] = useState("");
  const [tip, setTip] = useState<Tip | null>(null);
  const winRef = useRef(win);
  winRef.current = win;

  useEffect(() => {
    let live = true;
    const load = () => {
      const w = winRef.current;
      Promise.all([
        api<Board>("GET", `/api/usage/people?window=${w}`),
        api<Burn>("GET", `/api/usage/burn?window=${w}`),
      ])
        .then(([p, b]) => {
          if (!live) return;
          setErr("");
          setPeople(p.subjects || []);
          setBurn(b.buckets || []);
          setAsOf(p.asOf || "");
        })
        .catch((e) => live && setErr(e.message)); // keep last-known data on a transient failure
    };
    load();
    const id = setInterval(load, REFRESH_MS);
    return () => {
      live = false;
      clearInterval(id);
    };
  }, [win]);

  const onHover = (t: Omit<Tip, "x" | "y"> | null, e?: React.MouseEvent) => {
    if (!t || !e) return setTip(null);
    setTip({ ...t, x: e.clientX, y: e.clientY });
  };

  const rows = buildModelRows(people, metric);
  const maxRow = Math.max(...rows.map((r) => r.total), 1);
  const teamTotal = people.reduce((a, p) => a + val(p, metric), 0);
  const peopleSorted = [...people].sort((a, b) => val(b, metric) - val(a, metric));
  const idxOf = new Map(people.map((p, i) => [p.id, i] as const));
  const fresh = agoFrom(asOf);

  return (
    <div>
      <AnimatePresence>{tip && <Tooltip tip={tip} />}</AnimatePresence>

      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-[28px] font-bold tracking-tight">Usage</h1>
          <div className="mt-1 flex items-center gap-2 text-[15px] text-muted">
            <span className={`inline-block h-2 w-2 rounded-full ${fresh.stale ? "bg-warn" : "bg-ok"}`} />
            {fresh.stale ? fresh.text : "live · " + fresh.text}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          <Seg value={win} onChange={setWin} options={[["5h", "5h"], ["day", "Day"], ["week", "Week"], ["month", "Month"]]} />
          <Seg value={metric} onChange={setMetric} options={[["weighted", "Weighted"], ["cost", "USD"]]} />
        </div>
      </div>

      {err && <div className="mt-4 rounded-xl border border-crit/30 bg-crit/5 px-4 py-2.5 text-[14px] text-crit">{err}</div>}

      {/* Team headline + burn */}
      <div className="mt-6 grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
        <div className="rounded-2xl border border-line bg-raised p-5">
          <div className="text-[13px] font-medium uppercase tracking-[0.08em] text-faint">Team this {win === "5h" ? "5h" : win}</div>
          <div className="mt-1.5 text-[34px] font-bold leading-none tabular-nums">{fmt(teamTotal, metric)}</div>
          <div className="mt-1 text-[13.5px] text-muted">{metric === "weighted" ? "weighted tokens" : "spend-equivalent"} across {people.length} {people.length === 1 ? "person" : "people"}</div>
        </div>
        <div className="rounded-2xl border border-line bg-raised p-5">
          <div className="flex items-center justify-between text-[13px] font-medium uppercase tracking-[0.08em] text-faint">
            <span>Per hour</span>
          </div>
          <div className="mt-2 h-[56px]">
            <Sparkline buckets={burn} metric={metric} />
          </div>
        </div>
      </div>

      {/* People legend */}
      {peopleSorted.length > 0 && (
        <div className="mt-6 flex flex-wrap gap-2.5">
          {peopleSorted.map((p) => {
            const share = teamTotal > 0 ? val(p, metric) / teamTotal : 0;
            return (
              <div key={p.id} className="group inline-flex items-center gap-2 rounded-full border border-line bg-raised px-3 py-1.5 text-[14px] transition-colors hover:border-line/0 hover:bg-raised-2">
                <span className="h-2.5 w-2.5 rounded-full" style={{ background: personColor(idxOf.get(p.id) ?? 0) }} />
                <span className="font-medium text-ink">{p.name}</span>
                <span className="tabular-nums text-faint">{fmt(val(p, metric), metric)} · {Math.round(share * 100)}%</span>
              </div>
            );
          })}
        </div>
      )}

      {/* Where it's going — bars by model, sliced by person */}
      <h2 className="mb-1 mt-8 text-[13px] font-semibold uppercase tracking-[0.08em] text-muted">Where it's going</h2>
      <div className="text-[13px] text-faint">Each bar is a model; each colour a person. Hover a slice.</div>
      <div className="mt-3">
        {rows.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-line px-6 py-10 text-center">
            <div className="text-[16px] font-medium text-ink">No usage in this window yet</div>
            <div className="mx-auto mt-1.5 max-w-md text-[14px] leading-relaxed text-muted">
              It fills in as people run shared accounts through the gateway. A window where every request was rate-limited records nothing — those don't count against usage.
            </div>
          </div>
        ) : (
          rows.map((r) => <ModelBar key={r.label} row={r} max={maxRow} metric={metric} onHover={onHover} />)
        )}
      </div>
    </div>
  );
}
