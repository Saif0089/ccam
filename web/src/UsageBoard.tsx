import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { api, Board, Burn, Subject } from "./api";
import { fmtNum, modelColor, MODELS, agoFrom } from "./format";

type Metric = "weighted" | "cost";
type Window = "5h" | "day" | "week" | "month";

const subjVal = (s: Subject, m: Metric) => (m === "cost" ? s.costUsd : s.weighted);
const fmtMetric = (v: number, m: Metric) => (m === "cost" ? "$" + (v || 0).toFixed(2) : fmtNum(v || 0));

function Seg<T extends string>({ value, onChange, options }: { value: T; onChange: (v: T) => void; options: [T, string][] }) {
  return (
    <div className="inline-flex rounded-lg border border-line bg-sunken p-[3px]">
      {options.map(([v, label]) => (
        <button
          key={v}
          onClick={() => onChange(v)}
          className={`rounded-md px-3 py-[5px] text-sm transition-colors ${
            v === value ? "bg-raised-2 text-ink" : "text-muted hover:text-ink"
          }`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function Bar({ subject, max, metric }: { subject: Subject; max: number; metric: Metric }) {
  const total = subjVal(subject, metric);
  return (
    <div className="grid grid-cols-[150px_1fr_108px] items-center gap-3.5 py-[9px]">
      <div className="truncate font-semibold">{subject.name}</div>
      <div className="flex h-5 overflow-hidden rounded-md bg-sunken">
        <motion.div
          className="flex h-full"
          initial={{ width: 0 }}
          animate={{ width: `${(total / max) * 100}%` }}
          transition={{ duration: 0.5, ease: [0.2, 0.8, 0.2, 1] }}
        >
          {subject.byModel
            .filter((m) => (metric === "cost" ? m.costUsd : m.weighted) > 0)
            .map((m, i) => {
              const mv = metric === "cost" ? m.costUsd : m.weighted;
              return (
                <span
                  key={i}
                  className="h-full"
                  style={{ width: `${(mv / total) * 100}%`, background: modelColor(m.model).color }}
                  title={`${modelColor(m.model).label}: ${fmtMetric(mv, metric)}`}
                />
              );
            })}
        </motion.div>
      </div>
      <div className="text-right tabular-nums text-muted">
        <b className="font-semibold text-ink">{fmtMetric(total, metric)}</b>
      </div>
    </div>
  );
}

function SubjectBoard({ subjects, metric }: { subjects: Subject[]; metric: Metric }) {
  if (!subjects.length) return <div className="py-4 text-[15px] text-faint">Nothing in this window yet.</div>;
  const max = Math.max(...subjects.map((s) => subjVal(s, metric)), 1);
  return (
    <div className="flex flex-col gap-[2px]">
      {subjects.map((s) => (
        <Bar key={s.id} subject={s} max={max} metric={metric} />
      ))}
    </div>
  );
}

function Sparkline({ buckets, metric }: { buckets: Burn["buckets"]; metric: Metric }) {
  if (buckets.length < 2) return null;
  const vals = buckets.map((b) => (metric === "cost" ? b.costUsd : b.weighted));
  const max = Math.max(...vals, 1), n = vals.length, W = 100, H = 30;
  const line = vals.map((v, i) => `${(i / (n - 1)) * W},${(H - (v / max) * H).toFixed(2)}`).join(" ");
  return (
    <div className="mt-3.5">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block h-[52px] w-full">
        <polygon points={`0,${H} ${line} ${W},${H}`} fill="#6E8BFF" opacity={0.12} />
        <motion.polyline
          points={line}
          fill="none"
          stroke="#6E8BFF"
          strokeWidth={1.4}
          vectorEffect="non-scaling-stroke"
          initial={{ pathLength: 0 }}
          animate={{ pathLength: 1 }}
          transition={{ duration: 0.7 }}
        />
      </svg>
      <div className="mt-1 text-xs text-faint">Team usage per hour over the window</div>
    </div>
  );
}

export function UsageBoard() {
  const [win, setWin] = useState<Window>("day");
  const [metric, setMetric] = useState<Metric>("weighted");
  const [people, setPeople] = useState<Subject[]>([]);
  const [accounts, setAccounts] = useState<Subject[]>([]);
  const [burn, setBurn] = useState<Burn["buckets"]>([]);
  const [asOf, setAsOf] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    let live = true;
    setErr("");
    Promise.all([
      api<Board>("GET", `/api/usage/people?window=${win}`),
      api<Board>("GET", `/api/usage/accounts?window=${win}`),
      api<Burn>("GET", `/api/usage/burn?window=${win}`),
    ])
      .then(([p, a, b]) => {
        if (!live) return;
        setPeople(p.subjects || []);
        setAccounts(a.subjects || []);
        setBurn(b.buckets || []);
        setAsOf(p.asOf || a.asOf || "");
      })
      .catch((e) => live && setErr(e.message));
    return () => {
      live = false;
    };
  }, [win]);

  const fresh = agoFrom(asOf);
  const legendLabels = new Set<string>();
  for (const s of [...people, ...accounts]) for (const m of s.byModel) legendLabels.add(modelColor(m.model).label);

  return (
    <div>
      <div className="flex flex-wrap items-start gap-5">
        <div>
          <h1 className="text-2xl font-semibold">Usage</h1>
          <div className="mt-1 text-[15px] text-muted">
            Who used what, and on which model — <span className={fresh.stale ? "text-warn" : ""}>{fresh.text}</span>
          </div>
        </div>
        <div className="ml-auto flex flex-wrap items-center justify-end gap-2.5">
          <Seg value={win} onChange={setWin} options={[["5h", "5h"], ["day", "Day"], ["week", "Week"], ["month", "Month"]]} />
          <Seg value={metric} onChange={setMetric} options={[["weighted", "Weighted"], ["cost", "Cost"]]} />
        </div>
      </div>

      {err && <div className="mt-4 text-[15px] text-crit">{err}</div>}

      <div className="mt-2 flex flex-wrap gap-4 text-[13px] text-muted">
        {MODELS.filter((m) => legendLabels.has(m.label)).map((m) => (
          <span key={m.label} className="inline-flex items-center gap-1.5">
            <span className="h-[11px] w-[11px] rounded-[3px]" style={{ background: m.color }} />
            {m.label}
          </span>
        ))}
      </div>

      <Sparkline buckets={burn} metric={metric} />

      <h2 className="mb-2.5 mt-7 text-sm font-semibold uppercase tracking-[0.06em] text-muted">People</h2>
      <SubjectBoard subjects={people} metric={metric} />
      <h2 className="mb-2.5 mt-7 text-sm font-semibold uppercase tracking-[0.06em] text-muted">Accounts</h2>
      <SubjectBoard subjects={accounts} metric={metric} />

      <Quotas />
    </div>
  );
}

// --- quotas ----------------------------------------------------------------

interface LimitRow { limit: { id: string; subjectType: string; subjectId: string; windowKind: string; maxWeighted?: number; maxCostUsd?: number }; name: string }

function Quotas() {
  const [rows, setRows] = useState<LimitRow[]>([]);
  const [people, setPeople] = useState<{ id: string; name: string }[]>([]);
  const [subject, setSubject] = useState("org");
  const [windowKind, setWindowKind] = useState("day");
  const [weighted, setWeighted] = useState("");
  const [cost, setCost] = useState("");
  const [err, setErr] = useState("");

  const load = () => {
    Promise.all([api<{ limits: LimitRow[] }>("GET", "/api/limits"), api<{ people: { id: string; name: string }[] }>("GET", "/api/panel")])
      .then(([l, p]) => {
        setRows(l.limits || []);
        setPeople(p.people || []);
      })
      .catch((e) => setErr(e.message));
  };
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    const body: any = { windowKind };
    if (subject === "org") body.subjectType = "org";
    else {
      body.subjectType = "person";
      body.subjectId = subject.slice("person:".length);
    }
    const w = parseFloat(weighted);
    if (w > 0) body.maxWeighted = w;
    const c = parseFloat(cost);
    if (c > 0) body.maxCostUsd = c;
    try {
      await api("POST", "/api/limits", body);
      setWeighted("");
      setCost("");
      load();
    } catch (e: any) {
      setErr(e.message);
    }
  };

  const remove = async (id: string) => {
    try {
      await api("DELETE", "/api/limits/" + id);
      load();
    } catch (e: any) {
      setErr(e.message);
    }
  };

  return (
    <div>
      <h2 className="mb-2.5 mt-7 text-sm font-semibold uppercase tracking-[0.06em] text-muted">Quotas</h2>
      {rows.length === 0 && <div className="py-4 text-[15px] text-faint">No quotas set — everyone shares the subscription freely.</div>}
      {rows.map((r) => {
        const caps: string[] = [];
        if (r.limit.maxWeighted) caps.push(fmtNum(r.limit.maxWeighted) + " weighted");
        if (r.limit.maxCostUsd) caps.push("$" + r.limit.maxCostUsd);
        return (
          <div key={r.limit.id} className="grid grid-cols-[150px_1fr_auto] items-center gap-3.5 py-[9px]">
            <div className="truncate font-semibold">{r.name}</div>
            <div className="text-[15px] text-muted">{caps.join(" / ")} · per {r.limit.windowKind}</div>
            <button onClick={() => remove(r.limit.id)} className="rounded-md border border-crit/40 px-[11px] py-[5px] text-[13px] text-crit hover:bg-crit/10">
              Remove
            </button>
          </div>
        );
      })}
      <form onSubmit={submit} className="mt-3.5 flex flex-wrap items-center gap-2.5">
        <select value={subject} onChange={(e) => setSubject(e.target.value)} className="rounded-lg border border-line bg-sunken px-2.5 py-2 text-[15px]">
          <option value="org">The whole team</option>
          {people.map((p) => (
            <option key={p.id} value={"person:" + p.id}>{p.name}</option>
          ))}
        </select>
        <select value={windowKind} onChange={(e) => setWindowKind(e.target.value)} className="rounded-lg border border-line bg-sunken px-2.5 py-2 text-[15px]">
          <option value="day">per day</option>
          <option value="week">per week</option>
          <option value="month">per month</option>
        </select>
        <input value={weighted} onChange={(e) => setWeighted(e.target.value)} type="number" min={0} step={1000} placeholder="max weighted tokens" className="w-44 rounded-lg border border-line bg-sunken px-2.5 py-2 text-[15px]" />
        <input value={cost} onChange={(e) => setCost(e.target.value)} type="number" min={0} step={0.5} placeholder="max $ (optional)" className="w-44 rounded-lg border border-line bg-sunken px-2.5 py-2 text-[15px]" />
        <button type="submit" className="rounded-lg border border-line bg-raised-2 px-4 py-2 text-[15px] text-ink hover:border-primary/50">Set quota</button>
      </form>
      {err && <div className="mt-2 text-[14px] text-crit">{err}</div>}
    </div>
  );
}
