import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { api } from "./api";
import { fmtNum } from "./format";

interface LimitRow {
  limit: { id: string; subjectType: string; subjectId: string; windowKind: string; maxWeighted?: number; maxCostUsd?: number; maxPercent?: number };
  name: string;
  fraction?: number;
  resetAt?: string;
}

function capLabel(l: LimitRow["limit"]): string {
  const caps: string[] = [];
  if (l.maxPercent) caps.push(`${Math.round(l.maxPercent * 100)}% of the weekly window`);
  if (l.maxWeighted) caps.push(fmtNum(l.maxWeighted) + " weighted");
  if (l.maxCostUsd) caps.push("$" + l.maxCostUsd);
  return caps.join(" / ") + " · per " + l.windowKind;
}

// usageTone maps a limit's utilisation to a colour and a word: green under 75%,
// amber approaching (75–95%), red near or over the cap — the 75/95 marks the
// gateway warns and blocks at.
function usageTone(frac: number): { color: string; label: string } {
  if (frac >= 1) return { color: "#E05C53", label: "over the cap" };
  if (frac >= 0.95) return { color: "#E05C53", label: `${Math.round(frac * 100)}% — at the cap` };
  if (frac >= 0.75) return { color: "#E0A83E", label: `${Math.round(frac * 100)}% — approaching` };
  return { color: "#46C08A", label: `${Math.round(frac * 100)}% used` };
}

type Mode = "percent" | "weighted" | "cost";

export function Quotas() {
  const [rows, setRows] = useState<LimitRow[]>([]);
  const [people, setPeople] = useState<{ id: string; name: string }[]>([]);
  const [mode, setMode] = useState<Mode>("percent");
  const [subject, setSubject] = useState("org");
  const [windowKind, setWindowKind] = useState("week");
  const [percent, setPercent] = useState("");
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
    const id = setInterval(load, 12000);
    return () => clearInterval(id);
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
    if (mode === "percent") {
      const p = parseFloat(percent);
      if (!(p > 0 && p <= 100)) return setErr("Enter a percent between 0 and 100.");
      if (subject === "org") return setErr("A % of the weekly window applies to a person — pick one above.");
      body.maxPercent = p; // 0..100; the server normalises + forces a weekly window
    } else if (mode === "weighted") {
      const w = parseFloat(weighted);
      if (!(w > 0)) return setErr("Enter a weighted-token cap.");
      body.maxWeighted = w;
    } else {
      const c = parseFloat(cost);
      if (!(c > 0)) return setErr("Enter a USD cap.");
      body.maxCostUsd = c;
    }
    try {
      await api("POST", "/api/limits", body);
      setPercent("");
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
      <h1 className="text-[28px] font-bold tracking-tight">Quotas</h1>
      <div className="mt-1 text-[15px] text-muted">Cap what a person, or the whole team, can spend in a window. Over the cap, the gateway turns them away — the same way a real spend limit does.</div>

      <h2 className="mb-2.5 mt-8 text-[13px] font-semibold uppercase tracking-[0.08em] text-muted">In effect</h2>
      {rows.length === 0 && (
        <div className="rounded-2xl border border-dashed border-line px-6 py-8 text-center text-[15px] text-muted">
          No quotas yet — everyone shares the subscription freely. Set one below.
        </div>
      )}
      <div className="flex flex-col gap-2">
        {rows.map((r) => {
          const frac = r.fraction || 0;
          const tone = usageTone(frac);
          return (
            <div key={r.limit.id} className="rounded-2xl border border-line bg-raised p-4">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[16px] font-semibold">{r.name}</div>
                  <div className="text-[14px] text-muted">{capLabel(r.limit)}</div>
                </div>
                <div className="text-right">
                  <div className="text-[15px] font-semibold" style={{ color: tone.color }}>{tone.label}</div>
                </div>
                <button onClick={() => remove(r.limit.id)} className="rounded-lg border border-line px-3 py-1.5 text-[13px] text-faint transition-colors hover:border-crit/50 hover:text-crit">Remove</button>
              </div>
              <div className="mt-3 h-2 overflow-hidden rounded-full bg-sunken">
                <motion.div className="h-full rounded-full" style={{ background: tone.color }} initial={{ width: 0 }} animate={{ width: `${Math.min(frac, 1) * 100}%` }} transition={{ duration: 0.5, ease: [0.2, 0.8, 0.2, 1] }} />
              </div>
            </div>
          );
        })}
      </div>

      <h2 className="mb-2.5 mt-8 text-[13px] font-semibold uppercase tracking-[0.08em] text-muted">Set a quota</h2>
      <form onSubmit={submit} className="rounded-2xl border border-line bg-raised p-5">
        {/* Cap type — % of the weekly window leads; the raw caps are for anyone who wants them. */}
        <div className="inline-flex rounded-xl border border-line bg-sunken p-1">
          {([["percent", "% of weekly"], ["weighted", "Weighted tokens"], ["cost", "USD"]] as [Mode, string][]).map(([m, label]) => (
            <button type="button" key={m} onClick={() => setMode(m)} className={`rounded-lg px-3.5 py-1.5 text-[14px] font-medium transition-colors ${mode === m ? "bg-raised-2 text-ink" : "text-muted hover:text-ink"}`}>{label}</button>
          ))}
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-2.5">
          <select value={subject} onChange={(e) => setSubject(e.target.value)} className="rounded-lg border border-line bg-sunken px-3 py-2.5 text-[15px] outline-none focus:border-primary/60">
            {mode !== "percent" && <option value="org">The whole team</option>}
            {people.map((p) => (
              <option key={p.id} value={"person:" + p.id}>{p.name}</option>
            ))}
          </select>

          {mode === "percent" ? (
            <div className="flex items-center gap-2 rounded-lg border border-line bg-sunken px-3 py-1.5">
              <input value={percent} onChange={(e) => setPercent(e.target.value)} type="number" min={1} max={100} step={1} placeholder="25" className="w-16 bg-transparent text-[16px] font-semibold tabular-nums outline-none" />
              <span className="text-[15px] text-muted">% of the weekly window</span>
            </div>
          ) : (
            <>
              <select value={windowKind} onChange={(e) => setWindowKind(e.target.value)} className="rounded-lg border border-line bg-sunken px-3 py-2.5 text-[15px] outline-none focus:border-primary/60">
                <option value="day">per day</option>
                <option value="week">per week</option>
                <option value="month">per month</option>
              </select>
              {mode === "weighted" ? (
                <input value={weighted} onChange={(e) => setWeighted(e.target.value)} type="number" min={0} placeholder="max weighted tokens" className="min-w-[190px] flex-1 rounded-lg border border-line bg-sunken px-3 py-2.5 text-[15px] outline-none focus:border-primary/60" />
              ) : (
                <input value={cost} onChange={(e) => setCost(e.target.value)} type="number" min={0} step={0.5} placeholder="max $" className="min-w-[150px] flex-1 rounded-lg border border-line bg-sunken px-3 py-2.5 text-[15px] outline-none focus:border-primary/60" />
              )}
            </>
          )}

          <button type="submit" className="rounded-lg bg-primary px-5 py-2.5 text-[15px] font-semibold text-sunken transition-transform active:scale-[0.98]">Set quota</button>
        </div>
        {mode === "percent" && (
          <div className="mt-2.5 text-[13px] text-faint">A person's share of the account's weekly window — from Claude's real /usage numbers. It starts enforcing once usage is flowing.</div>
        )}
      </form>
      {err && <div className="mt-2 text-[14px] text-crit">{err}</div>}
    </div>
  );
}
