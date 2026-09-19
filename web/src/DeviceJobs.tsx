import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { api, Job } from "./api";
import { when } from "./format";

// The remote-help panel for one machine: ask it to look at itself (diagnose,
// list its sessions, send a transcript) and watch the answers come back. Only
// ever opened for a machine whose owner turned remote help on. Results arrive on
// the machine's next check-in, so this polls while it is open.
export function DeviceJobs({ deviceId, deviceName, onClose }: { deviceId: string; deviceName: string; onClose: () => void }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [session, setSession] = useState("");
  const [folder, setFolder] = useState("~");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = () =>
    api<{ jobs: Job[] }>("GET", `/api/devices/${deviceId}/jobs`)
      .then((r) => setJobs(r.jobs || []))
      .catch((e) => setErr(e.message));

  useEffect(() => {
    load();
    // Answers land when the machine next checks in (~30s); poll so they appear.
    timer.current = setInterval(load, 4000);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => {
      if (timer.current) clearInterval(timer.current);
      window.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const ask = async (kind: string, params?: string) => {
    setBusy(true);
    setErr("");
    try {
      await api("POST", `/api/devices/${deviceId}/jobs`, { kind, params });
      await load();
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const pending = jobs.some((j) => j.status === "pending");

  return (
    <motion.div
      className="fixed inset-0 z-50 flex items-center justify-center px-4"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.15 }}
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div className="absolute inset-0 bg-black/60 backdrop-blur-[2px]" />
      <motion.div
        initial={{ opacity: 0, scale: 0.96, y: 8 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.98 }}
        transition={{ duration: 0.18, ease: [0.2, 0.8, 0.2, 1] }}
        className="relative flex max-h-[85vh] w-full max-w-lg flex-col rounded-2xl border border-line bg-raised p-7 shadow-2xl"
      >
        <div className="flex items-baseline gap-3">
          <h2 className="text-xl font-semibold tracking-tight">Ask {deviceName}</h2>
          <span className="text-[13px] text-faint">remote help is on for this machine</span>
          <button onClick={onClose} className="ml-auto text-[15px] text-faint hover:text-ink">Close</button>
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-2">
          <button disabled={busy} onClick={() => ask("diagnose")} className="rounded-lg border border-line bg-raised-2 px-3.5 py-2 text-[15px] hover:border-primary/50 disabled:opacity-50">Diagnose</button>
          <button disabled={busy} onClick={() => ask("sessions")} className="rounded-lg border border-line bg-raised-2 px-3.5 py-2 text-[15px] hover:border-primary/50 disabled:opacity-50">List sessions</button>
        </div>
        <div className="mt-2 flex items-center gap-2">
          <input
            value={session}
            onChange={(e) => setSession(e.target.value)}
            placeholder="session id (from a session list)"
            className="min-w-0 flex-1 rounded-lg border border-line bg-sunken px-3 py-2 text-[14px] outline-none focus:border-primary/60"
          />
          <button
            disabled={busy || !session.trim()}
            onClick={() => ask("transcript", session.trim())}
            className="shrink-0 rounded-lg border border-line bg-raised-2 px-3.5 py-2 text-[15px] hover:border-primary/50 disabled:opacity-40"
          >
            Get transcript
          </button>
        </div>

        {/* File browser, scoped to the machine's ~/.claude — its Claude sessions and
            transcripts, never the rest of the disk. The machine refuses anything outside it. */}
        <div className="mt-2 flex items-center gap-2">
          <span className="text-[13px] text-faint">Claude files</span>
          <input
            value={folder}
            onChange={(e) => setFolder(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && folder.trim() && ask("ls", folder.trim())}
            placeholder="~  (a folder under ~/.claude)"
            className="min-w-0 flex-1 rounded-lg border border-line bg-sunken px-3 py-2 font-mono text-[13.5px] outline-none focus:border-primary/60"
          />
          <button
            disabled={busy || !folder.trim()}
            onClick={() => ask("ls", folder.trim())}
            className="shrink-0 rounded-lg border border-line bg-raised-2 px-3.5 py-2 text-[15px] hover:border-primary/50 disabled:opacity-40"
          >
            Browse
          </button>
        </div>

        {err && <div className="mt-3 text-[14px] text-crit">{err}</div>}

        <div className="mt-5 min-h-0 flex-1 overflow-y-auto">
          <div className="mb-2 flex items-center gap-2 text-sm font-semibold uppercase tracking-[0.06em] text-muted">
            Results
            {pending && <span className="text-[12px] font-normal normal-case text-faint">· waiting for the machine to check in…</span>}
          </div>
          {jobs.length === 0 && <div className="py-3 text-[14px] text-faint">Nothing asked yet. Diagnose it, list its sessions, or browse its Claude files.</div>}
          <div className="flex flex-col gap-3">
            {jobs.map((j) => (
              <div key={j.id} className="rounded-xl border border-line bg-sunken p-3">
                <div className="flex items-center gap-2 text-[14px]">
                  <span className="font-semibold">{jobLabel(j)}</span>
                  <JobBadge status={j.status} />
                  <span className="ml-auto text-[13px] text-faint">{when(j.createdAt)}</span>
                </div>
                {j.status === "done" && j.result && j.kind === "ls" ? (
                  <FolderView result={j.result} onOpen={(kind, path) => { if (kind === "ls") setFolder(path); ask(kind, path); }} />
                ) : j.status === "done" && j.result && j.kind === "get" ? (
                  <FileView result={j.result} />
                ) : j.status !== "pending" && j.result ? (
                  <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-ground p-3 font-mono text-[12.5px] leading-relaxed text-muted">{j.result}</pre>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      </motion.div>
    </motion.div>
  );
}

// FolderView renders an `ls` result as a navigable listing: folders reopen with
// another ls, files fetch with a get.
function FolderView({ result, onOpen }: { result: string; onOpen: (kind: "ls" | "get", path: string) => void }) {
  let d: { path: string; parent: string; entries: { name: string; dir: boolean; size: number; mod: string }[] };
  try {
    d = JSON.parse(result);
  } catch {
    return <pre className="mt-2 text-[12.5px] text-crit">{result}</pre>;
  }
  const join = (name: string) => (d.path.endsWith("/") ? d.path + name : d.path + "/" + name);
  const entries = [...(d.entries || [])].sort((a, b) => (a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1));
  return (
    <div className="mt-2">
      <div className="mb-1.5 flex items-center gap-2 font-mono text-[12.5px] text-faint">
        <span className="truncate">{d.path}</span>
        {d.parent && d.parent !== d.path && (
          <button onClick={() => onOpen("ls", d.parent)} className="ml-auto rounded px-1.5 py-0.5 text-[12px] text-primary hover:bg-primary/12">↑ up</button>
        )}
      </div>
      <div className="max-h-64 divide-y divide-line overflow-auto rounded-lg border border-line bg-ground">
        {entries.map((e) => (
          <button
            key={e.name}
            onClick={() => onOpen(e.dir ? "ls" : "get", join(e.name))}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[13px] hover:bg-raised-2"
          >
            <span className="w-4 shrink-0 text-center text-faint">{e.dir ? "📁" : "📄"}</span>
            <span className="truncate font-mono text-ink">{e.name}</span>
            <span className="ml-auto shrink-0 tabular-nums text-faint">{e.dir ? "" : humanBytes(e.size)}</span>
          </button>
        ))}
        {entries.length === 0 && <div className="px-3 py-2 text-[13px] text-faint">empty folder</div>}
      </div>
    </div>
  );
}

// FileView renders a `get` result: text inline, anything else as a download.
function FileView({ result }: { result: string }) {
  let d: { path: string; size: number; encoding: string; content: string; truncated: boolean };
  try {
    d = JSON.parse(result);
  } catch {
    return <pre className="mt-2 text-[12.5px] text-crit">{result}</pre>;
  }
  const download = () => {
    const name = d.path.split("/").pop() || "file";
    let href: string;
    if (d.encoding === "base64") href = "data:application/octet-stream;base64," + d.content;
    else href = "data:text/plain;charset=utf-8," + encodeURIComponent(d.content);
    const a = document.createElement("a");
    a.href = href;
    a.download = name;
    a.click();
  };
  return (
    <div className="mt-2">
      <div className="mb-1.5 flex items-center gap-2 text-[12.5px] text-faint">
        <span className="font-mono">{humanBytes(d.size)}{d.truncated ? " · showing the first 4 MB" : ""}</span>
        <button onClick={download} className="ml-auto rounded-lg border border-line bg-raised-2 px-2.5 py-1 text-[12.5px] text-ink hover:border-primary/50">Download</button>
      </div>
      {d.encoding === "utf8" ? (
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-ground p-3 font-mono text-[12.5px] leading-relaxed text-muted">{d.content}</pre>
      ) : (
        <div className="rounded-lg bg-ground p-3 text-[13px] text-faint">Binary file — use Download to save it.</div>
      )}
    </div>
  );
}

function humanBytes(n: number): string {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(1) + " KB";
  return n + " B";
}

function jobLabel(j: Job): string {
  const base =
    j.kind === "diagnose" ? "Health check" : j.kind === "sessions" ? "Session list" : j.kind === "transcript" ? "Transcript" : j.kind === "ls" ? "Folder" : j.kind === "get" ? "File" : j.kind;
  return j.params ? `${base} · ${j.params}` : base;
}

function JobBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    pending: "text-warn border-warn/40",
    done: "text-ok border-ok/40",
    error: "text-crit border-crit/40",
  };
  return <span className={`rounded-full border px-2 py-[1px] text-[11px] ${map[status] || "text-faint border-line"}`}>{status}</span>;
}
