import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { api, Panel, Account, Person, Device } from "./api";
import { ClawIntro } from "./ClawIntro";
import { UsageBoard } from "./UsageBoard";
import { useDialog } from "./Dialog";
import { DeviceJobs } from "./DeviceJobs";
import { when, untilExpiry } from "./format";

type Ask = ReturnType<typeof useDialog>["ask"];

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
  const { ask, node } = useDialog();
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
  return (
    <>
      {tab === "accounts" && <Accounts data={data} reload={load} ask={ask} />}
      {tab === "people" && <People data={data} reload={load} ask={ask} />}
      {tab === "activity" && <Activity data={data} />}
      {node}
    </>
  );
}

// run does a thing, reloads, and surfaces any refusal in a dialog.
async function run(fn: () => Promise<unknown>, reload: () => void, ask: Ask) {
  try {
    await fn();
    reload();
  } catch (e: any) {
    await ask({ title: "That didn't work", note: e.message, confirm: "Close", cancel: "" });
  }
}

// showInvite presents an invite link with a copy button and its expiry.
async function showInvite(ask: Ask, name: string, url: string, expiresAt?: string) {
  await ask({
    title: `Invite for ${name}`,
    fields: [{ name: "link", label: "Send them this link", copy: url }],
    note: `Works once, ${untilExpiry(expiresAt)}. They open it, join in a click, and whatever you share shows up on their machine.`,
    confirm: "Done",
    cancel: "",
  });
}

function Accounts({ data, reload, ask }: { data: Panel; reload: () => void; ask: Ask }) {
  const ready = data.accounts.filter((a) => a.hasLogin).length;
  const need = data.accounts.length - ready;
  const sub = data.accounts.length ? `${ready} ready to share` + (need ? `, ${need} need a login` : "") : "";

  // giveAccess shares an account with people through the gateway. Many people
  // can use one account at once, and access ends the instant it is taken back.
  const give = async (a: Account) => {
    const without = data.people.filter((p) => !(a.shared || []).some((s) => s.personId === p.id));
    if (!data.people.length) return void ask({ title: "Nobody to give it to", note: "Invite someone on the People tab first.", confirm: "Close", cancel: "" });
    if (!without.length) return void ask({ title: "Everyone already has it", note: "Everyone you've added can already use this account.", confirm: "Close", cancel: "" });
    const out = await ask({
      title: `Give access to ${a.name}`,
      fields: [{ name: "people", label: "To", checks: without.map((p) => ({ value: p.id, label: p.name })) }],
      note: "Pick everyone who should use this account. Many people can share it at once, through the gateway — it appears on each machine once they've joined.",
      confirm: "Give access",
    });
    const ids = (out?.people as string[]) || [];
    if (!ids.length) return;
    try {
      let lastGateway = "";
      for (const id of ids) {
        const { gateway } = await api<{ gateway?: string }>("POST", `/api/accounts/${a.id}/share`, { personId: id });
        lastGateway = gateway || lastGateway;
      }
      reload();
      const names = without.filter((p) => ids.includes(p.id)).map((p) => p.name).join(", ");
      await ask(
        lastGateway
          ? { title: "Done", note: `${names} can now use ${a.name}. It appears on each machine within a minute once they've joined.`, confirm: "Close", cancel: "" }
          : { title: "Gateway not set", note: "There's no gateway configured (CLAWDH_GATEWAY_URL), so there's nowhere to point their access yet.", confirm: "Close", cancel: "" }
      );
    } catch (e: any) {
      await ask({ title: "That didn't work", note: e.message, confirm: "Close", cancel: "" });
    }
  };

  const revoke = async (a: Account, sh: { shareId: string; personName: string }) => {
    const ok = await ask({ title: `Take ${a.name} away from ${sh.personName}?`, note: "Their access stops within seconds, and any session they're running on it ends.", confirm: "Take it away", danger: true });
    if (ok) run(() => api("POST", `/api/shares/${sh.shareId}/revoke`), reload, ask);
  };

  // howToAddLogin explains the one thing the panel can't do itself: sign in.
  const howToAddLogin = (a: Account) =>
    ask({
      title: `Add a login for ${a.name}`,
      note: "The panel can't sign in for you — Claude's login needs a real browser. On the machine where this account is signed in, open the clawdh page there and choose “Add to panel” on it. That hands its login up here. People you share it with never do this.",
      confirm: "Got it",
      cancel: "",
    });

  const removeAccount = async (a: Account) => {
    const ok = await ask({ title: `Remove ${a.name}?`, note: (a.shared || []).length ? "Everyone using it loses access." : "", confirm: "Remove", danger: true });
    if (ok) run(() => api("DELETE", `/api/accounts/${a.id}`), reload, ask);
  };

  const addAccount = async () => {
    const out = await ask({
      title: "Add an account",
      fields: [
        { name: "name", label: "Name", placeholder: "Work" },
        { name: "email", label: "Its Claude sign-in (optional)", placeholder: "work@example.com" },
      ],
      note: "This makes an empty slot. To share it, add its login from the machine where it's signed in — see “How to add its login”.",
      confirm: "Add",
    });
    if (out?.name) run(() => api("POST", "/api/accounts", out), reload, ask);
  };

  return (
    <div>
      <Head
        title="Accounts"
        sub={sub}
        action={<button onClick={addAccount} className="rounded-lg border border-line bg-raised-2 px-4 py-2 font-semibold text-ink hover:border-primary/50">Add an account</button>}
      />
      {data.accounts.length === 0 && <Empty>No accounts yet. On a machine where a Claude account is signed in, open the clawdh page there and choose “Add to panel” on that account. It shows up here, ready to share.</Empty>}
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
                    <button onClick={() => revoke(a, sh)} className="text-faint hover:text-crit" title="Take access away">×</button>
                  </span>
                ))
              ) : (
                <Pill kind="ok">Ready to share</Pill>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-4">
              {a.hasLogin ? (
                <button onClick={() => give(a)} className="text-[15px] text-primary hover:underline">Give access</button>
              ) : (
                <button onClick={() => howToAddLogin(a)} className="text-[15px] text-primary hover:underline">How to add its login</button>
              )}
              <button onClick={() => removeAccount(a)} className="text-[15px] text-faint hover:text-crit">Remove</button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function People({ data, reload, ask }: { data: Panel; reload: () => void; ask: Ask }) {
  const setUp = data.people.filter((p) => (p.devices || []).length).length;
  const sub = data.people.length ? `${data.people.length} ${data.people.length === 1 ? "person" : "people"}, ${setUp} set up` : "";
  const [jobsFor, setJobsFor] = useState<Device | null>(null);

  // inviteSomeone adds a person and hands you their invite link in one step.
  const inviteSomeone = async () => {
    const out = await ask({
      title: "Invite someone",
      fields: [
        { name: "name", label: "Their name", placeholder: "Ehtisham" },
        { name: "email", label: "Email (optional)" },
      ],
      confirm: "Create invite",
    });
    if (!out?.name) return;
    try {
      await api("POST", "/api/people", { name: out.name, email: out.email });
      const target = (await api<Panel>("GET", "/api/panel")).people.find((p) => p.name === out.name);
      reload();
      if (!target) return;
      const { url, expiresAt } = await api<{ url: string; expiresAt?: string }>("POST", `/api/people/${target.id}/invite`);
      await showInvite(ask, String(out.name), url, expiresAt);
    } catch (e: any) {
      await ask({ title: "That didn't work", note: e.message, confirm: "Close", cancel: "" });
    }
  };

  // invitePerson makes a fresh invite link for someone already added.
  const invitePerson = async (p: Person) => {
    try {
      const { url, expiresAt } = await api<{ url: string; expiresAt?: string }>("POST", `/api/people/${p.id}/invite`);
      await showInvite(ask, p.name, url, expiresAt);
    } catch (e: any) {
      await ask({ title: "That didn't work", note: e.message, confirm: "Close", cancel: "" });
    }
  };

  const removePerson = async (p: Person) => {
    const ok = await ask({ title: `Remove ${p.name}?`, note: "Their access ends and their machines stop working straight away.", confirm: "Remove", danger: true });
    if (ok) run(() => api("DELETE", `/api/people/${p.id}`), reload, ask);
  };

  const cutOff = async (p: Person, d: Device) => {
    const ok = await ask({ title: `Remove ${d.name}?`, note: `${p.name}'s other machines keep working.`, confirm: "Remove", danger: true });
    if (ok) run(() => api("DELETE", `/api/devices/${d.id}`), reload, ask);
  };

  return (
    <div>
      <Head
        title="People"
        sub={sub}
        action={<button onClick={inviteSomeone} className="rounded-lg bg-primary px-4 py-2 font-semibold text-sunken">Invite someone</button>}
      />
      {data.people.length === 0 && <Empty>Nobody yet. Invite someone — they get a link, join in a click, and whatever you share appears on their machine.</Empty>}
      <div className="flex flex-col divide-y divide-line">
        {data.people.map((p: Person) => (
          <div key={p.id} className="flex flex-wrap items-start gap-4 py-3.5">
            <div className="min-w-[150px] flex-1">
              <div className="font-semibold">{p.name}</div>
              <div className="text-[14px] text-faint">{p.email}</div>
            </div>
            <div className="min-w-[130px] flex-1 text-[14px]">
              <span className={(p.can || []).length ? "text-muted" : "text-faint"}>{(p.can || []).join(", ") || "Nothing yet"}</span>
            </div>
            <div className="min-w-[170px] flex-1">
              {(p.devices || []).length ? (
                (p.devices || []).map((d) => (
                  <div key={d.id} className="flex items-baseline gap-2.5 py-0.5 text-[13.5px]">
                    <span className="font-mono">{d.name}</span>
                    {d.remote && <span title="Remote help is on — you can ask this machine to look at itself" className="text-[11px] text-primary">◆ remote</span>}
                    <span className="text-faint">{when(d.lastSeen)}</span>
                    <div className="ml-auto flex gap-2.5">
                      {d.remote && <button onClick={() => setJobsFor(d)} className="text-[13px] text-primary hover:underline">Ask…</button>}
                      <button onClick={() => cutOff(p, d)} className="text-[13px] text-faint hover:text-crit">Remove</button>
                    </div>
                  </div>
                ))
              ) : (
                <span className="text-[14px] text-faint">Not joined yet</span>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-4">
              <button onClick={() => invitePerson(p)} className="text-[15px] text-primary hover:underline">Invite</button>
              <button onClick={() => removePerson(p)} className="text-[15px] text-faint hover:text-crit">Remove</button>
            </div>
          </div>
        ))}
      </div>
      <AnimatePresence>
        {jobsFor && <DeviceJobs key={jobsFor.id} deviceId={jobsFor.id} deviceName={jobsFor.name} onClose={() => setJobsFor(null)} />}
      </AnimatePresence>
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
