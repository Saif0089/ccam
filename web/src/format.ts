export function fmtNum(n: number): string {
  n = Math.round(n || 0);
  if (n >= 1e9) return (n / 1e9).toFixed(1) + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "K";
  return String(n);
}

export const MODELS = [
  { test: /fable|mythos/, color: "#C77DFF", label: "Fable / Mythos" },
  { test: /opus/, color: "#6E8BFF", label: "Opus" },
  { test: /sonnet/, color: "#46C08A", label: "Sonnet" },
  { test: /haiku/, color: "#E0A83E", label: "Haiku" },
  { test: /unknown/, color: "#626D7C", label: "Unknown" },
];
export function modelColor(m: string) {
  const lm = (m || "").toLowerCase();
  for (const c of MODELS) if (c.test.test(lm)) return c;
  return { color: "#8892A0", label: m || "?" };
}

export function agoFrom(iso: string): { text: string; stale: boolean } {
  if (!iso || iso.startsWith("0001")) return { text: "no usage recorded yet", stale: false };
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  return { text: "as of " + (mins < 1 ? "just now" : mins + "m ago"), stale: mins > 2 };
}
