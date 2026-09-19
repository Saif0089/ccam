import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { api, Panel, Account, Person } from "./api";
import { ClawIntro } from "./ClawIntro";
import { UsageBoard } from "./UsageBoard";

type Tab = "accounts" | "people" | "usage" | "activity";
type Status = "loading" | "setup" | "gate" | "in";

export default function App() {
  const [intro, setIntro] = useState(true);
  const [status, setStatus] = useState<Status>("loading");
  const [tab, setTab] = useState<Tab>("accounts");

  useEffect(() => {
    api<{ needsSetup: boolean; signedIn: boolean }>("GET", "/api/status")
      .then((s) => setStatus(s.signedIn ? "in" : s.needsSetup ? "setup" : "gate"))
      .catch(() => setStatus("gate"));
  }, []);

  return (
    <div className="min-h-full font-sans text-ink">
      <AnimatePresence>{intro && <ClawIntro onDone={() => setIntro(false)} />}</AnimatePresence>
      {status === "loading" ? null : status === "in" ? (
        <Shell tab={tab} setTab={setTab} onSignOut={() => setStatus("gate")} />
      ) : (
        <Gate setup={status === "setup"} onIn={() => setStatus("in")} />
      )}
    </div>
  );
}

function Gate({ setup, onIn }: { setup: boolean; onIn: () => void }) {
  const [pw, setPw] = useState("");
  const [err, setErr] = useState("");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await api("POST", setup ? "/api/setup" : "/api/login", { password: pw });
      onIn();
    } catch (e: any) {
      setErr(e.message);
    }
  };
  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <motion.form
        onSubmit={submit}
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 1.4, duration: 0.4 }}
        className="w-full max-w-sm rounded-2xl border border-line bg-raised p-8"
      >
        <div className="text-2xl font-semibold tracking-tight">clawdh</div>
        <div className="mt-1 text-[15px] text-muted">
          {setup ? "Set an admin password to run this panel." : "Sign in to the panel."}
        </div>
        <input
          type="password"
          autoFocus
          value={pw}
          onChange={(e) => setPw(e.target.value)}
          placeholder="Password"
          className="mt-5 w-full rounded-xl border border-line bg-sunken px-4 py-3 text-[16px] outline-none focus:border-primary/60"
        />
        <button className="mt-4 w-full rounded-xl bg-primary py-3 font-semibold text-sunken transition-transform active:scale-[0.98]">
          {setup ? "Create panel" : "Sign in"}
        </button>
        <div className="mt-3 min-h-[1.2em] text-sm text-crit">{err}</div>
      </motion.form>
    </div>
  );
}

function Shell({ tab, setTab, onSignOut }: { tab: Tab; setTab: (t: Tab) => void; onSignOut: () => void }) {
  const signOut = async () => {
    try {
      await api("POST", "/api/logout");
    } catch {
      /* ignore */
    }
    onSignOut();
  };
  const tabs: [Tab, string][] = [["accounts", "Accounts"], ["people", "People"], ["usage", "Usage"], ["activity", "Activity"]];
  return (
    <div className="mx-auto max-w-4xl px-5 py-6">
      <div className="flex items-center gap-6 border-b border-line pb-3">
        <span className="text-xl font-semibold tracking-tight">clawdh</span>
        <nav className="flex gap-1">
          {tabs.map(([t, label]) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`rounded-md px-3 py-1.5 text-[16px] transition-colors ${
                t === tab ? "text-ink" : "text-muted hover:text-ink"
              }`}
            >
              {label}
            </button>
          ))}
        </nav>
        <button onClick={signOut} className="ml-auto text-[15px] text-faint hover:text-ink">
          Sign out
        </button>
      </div>
      <motion.div key={tab} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.25 }} className="pt-6">
        {tab === "usage" ? <UsageBoard /> : <PanelTab tab={tab} />}
      </motion.div>
    </div>
  );
}

function PanelTab({ tab }: { tab: Tab }) {
  const [data, setData] = useState<Panel | null>(null);
  const [err, setErr] = useState("");
  const load = () =>
    api<Panel>("GET", "/api/panel")
      .then(setData)
      .catch((e) => setErr(e.message));
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  if (err) return <div className="text-[15px] text-crit">{err}</div>;
  if (!data) return <div className="text-[15px] text-faint">Loading…</div>;
  if (tab === "accounts") return <Accounts data={data} reload={load} />;
  if (tab === "people") return <People data={data} reload={load} />;
  return <Activity data={data} />;
}

function Accounts({ data, reload }: { data: Panel; reload: () => void }) {
  const give = async (a: Account) => {
    const person = prompt(`Give ${a.name} to which person? (type their exact name)`);
    if (!person) return;
    const p = data.people.find((x) => x.name.toLowerCase() === person.toLowerCase());
    if (!p) return alert("No person by that name — add them in the People tab first.");
    try {
      await api("POST", `/api/accounts/${a.id}/share`, { personId: p.id });
      reload();
    } catch (e: any) {
      alert(e.message);
    }
  };
  const revoke = async (shareId: string) => {
    try {
      await api("POST", `/api/shares/${shareId}/revoke`, {});
      reload();
    } catch (e: any) {
      alert(e.message);
    }
  };
  return (
    <div>
      <Head title="Accounts" sub={`${data.accounts.filter((a) => a.hasLogin).length} ready to share`} />
      {data.accounts.length === 0 && <Empty>No accounts yet. On a machine where a Claude account is signed in, open its clawdh page and choose “Add to panel”.</Empty>}
      <div className="flex flex-col divide-y divide-line">
        {data.accounts.map((a) => (
          <div key={a.id} className="flex items-center gap-4 py-3.5">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 font-semibold">
                <span className="truncate">{a.name}</span>
                {a.warning && <span title={a.warning} className="cursor-help text-warn">⚠</span>}
              </div>
              <div className="text-[14px] text-faint">{a.email || (a.plan ? a.plan + " plan" : "")}</div>
            </div>
            <div className="flex min-w-0 flex-1 flex-wrap gap-1.5">
              {!a.hasLogin ? (
                <Pill kind="need">Needs a login</Pill>
              ) : (a.shared || []).length ? (
                (a.shared || []).map((sh) => (
                  <span key={sh.shareId} className="inline-flex items-center gap-1.5 rounded-md border border-line bg-raised-2 px-2 py-1 text-[14px]">
                    {sh.personName}
                    <button onClick={() => revoke(sh.shareId)} className="text-faint hover:text-crit" title="Take access away">×</button>
                  </span>
                ))
              ) : (
                <Pill kind="ok">Ready to share</Pill>
              )}
            </div>
            {a.hasLogin && (
              <button onClick={() => give(a)} className="text-[15px] text-primary hover:underline">
                Give access
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function People({ data, reload }: { data: Panel; reload: () => void }) {
  const invite = async () => {
    const name = prompt("Who are you inviting? (their name)");
    if (!name) return;
    try {
      await api("POST", "/api/people", { name });
      const p = (await api<Panel>("GET", "/api/panel")).people.find((x) => x.name === name);
      if (p) {
        const res = await api<{ url: string }>("POST", `/api/people/${p.id}/invite`, {});
        prompt("Send them this link (it expires in about an hour):", res.url);
      }
      reload();
    } catch (e: any) {
      alert(e.message);
    }
  };
  return (
    <div>
      <Head
        title="People"
        sub={`${data.people.length} on the team`}
        action={<button onClick={invite} className="rounded-lg bg-primary px-4 py-2 font-semibold text-sunken">Invite someone</button>}
      />
      {data.people.length === 0 && <Empty>No one yet. Invite a teammate — they get a link that sets their machine up.</Empty>}
      <div className="flex flex-col divide-y divide-line">
        {data.people.map((p: Person) => (
          <div key={p.id} className="py-3.5">
            <div className="font-semibold">{p.name}</div>
            <div className="text-[14px] text-muted">
              {(p.can || []).length ? "Can use " + (p.can || []).join(", ") : "No access yet"}
              {(p.devices || []).length ? ` · ${(p.devices || []).length} machine(s)` : ""}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Activity({ data }: { data: Panel }) {
  return (
    <div>
      <Head title="Activity" sub="Everything that happened to an account" />
      <div className="flex flex-col">
        {(data.activity || []).map((e, i) => (
          <div key={i} className="flex gap-4 border-t border-line py-3.5 text-[15px]">
            <time className="w-32 shrink-0 tabular-nums text-faint">{new Date(e.at).toLocaleString()}</time>
            <span>
              <b className="font-semibold">{e.who}</b> {e.what}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function Head({ title, sub, action }: { title: string; sub: string; action?: React.ReactNode }) {
  return (
    <div className="mb-2 flex items-start gap-5">
      <div>
        <h1 className="text-2xl font-semibold">{title}</h1>
        <div className="mt-1 text-[15px] text-muted">{sub}</div>
      </div>
      {action && <div className="ml-auto">{action}</div>}
    </div>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="mt-6 rounded-xl border border-dashed border-line px-7 py-8 text-center text-[15px] leading-relaxed text-muted">{children}</div>;
}

function Pill({ kind, children }: { kind: "ok" | "need"; children: React.ReactNode }) {
  const c = kind === "ok" ? "text-ok" : "text-warn";
  return (
    <span className={`inline-flex items-center gap-1.5 text-[14px] ${c}`}>
      <span className={`h-[7px] w-[7px] rounded-full ${kind === "ok" ? "bg-ok" : "bg-warn"}`} />
      {children}
    </span>
  );
}
