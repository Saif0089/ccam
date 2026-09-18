package panel

import (
	"html"
	"strings"
)

// Install one-liners, shown on the invite page so a new person has real steps
// rather than "ask your admin". Kept here (not just the README) because the
// invite page is the first thing someone who has never seen ccam will read.
const (
	installRepo    = "https://github.com/Saif0089/ccam"
	claudeCodeURL  = "https://claude.com/claude-code"
	installUnixCmd = "curl -fsSL https://raw.githubusercontent.com/Saif0089/ccam/main/install.sh | sh"
	installWinCmd  = "irm https://raw.githubusercontent.com/Saif0089/ccam/main/install.ps1 | iex"
)

// invitePageHTML is the page an invite link opens in a browser. It is a single
// self-contained document: an invite is often opened by someone who has never
// seen ccam, so it explains itself, gives real install commands, and stands on
// its own. It shares the panel's colour tokens and type so the whole product
// looks like one thing.
func invitePageHTML(link, state string) string {
	esc := html.EscapeString(link)
	joinCmd := html.EscapeString("ccam join " + link)

	var body string
	switch state {
	case "expired", "used":
		reason := "This invite has expired."
		if state == "used" {
			reason = "This invite has already been used."
		}
		body = `
      <h1>Link no longer works</h1>
      <p class="lead">` + reason + ` Ask whoever sent it for a fresh one — invites last about an hour and work once.</p>`
	default:
		note := ""
		if state == "unknown" {
			note = `<p class="muted small">We couldn't confirm this link here. If it's from a different panel, these steps still apply.</p>`
		}
		body = `
      <h1>You've been invited to Claude</h1>
      <p class="lead">You'll run Claude Code on a shared account — no sign-in of your own, nothing to keep. Two steps and you're set.</p>

      <ol class="steps">
        <li>
          <span class="n">1</span>
          <div class="step-body">
            <b>Install ccam</b>
            <span class="det">Once per computer. Paste this into a terminal — it installs, starts in the background, and opens the ccam page.</span>
            <div class="os-tabs" role="tablist">
              <button type="button" class="os-tab is-on" data-os="unix">macOS / Linux</button>
              <button type="button" class="os-tab" data-os="win">Windows</button>
            </div>
            <div class="copybox" data-copy-for="install">
              <code id="install-cmd">` + html.EscapeString(installUnixCmd) + `</code>
              <button type="button" class="copy" data-copy-target="install-cmd">Copy</button>
            </div>
            <span class="det small">Needs the <a href="` + claudeCodeURL + `" target="_blank" rel="noopener">Claude Code CLI</a> first ·
              <a href="` + installRepo + `" target="_blank" rel="noopener">all install options</a></span>
          </div>
        </li>
        <li>
          <span class="n">2</span>
          <div class="step-body">
            <b>Paste your link</b>
            <span class="det">Open the ccam page the installer opened, find <b>Got an invite?</b>, and paste the link below. Done — shared accounts appear on their own.</span>
            <label class="fieldlabel">Your invite link</label>
            <div class="copybox big" data-copy-for="link">
              <code id="invite-link">` + esc + `</code>
              <button type="button" class="copy primary" data-copy-target="invite-link">Copy</button>
            </div>
          </div>
        </li>
      </ol>

      <details class="terminal">
        <summary>Prefer the terminal?</summary>
        <p class="det small">Skip the page — run this once after installing:</p>
        <div class="copybox" data-copy-for="join">
          <code id="join-cmd">` + joinCmd + `</code>
          <button type="button" class="copy" data-copy-target="join-cmd">Copy</button>
        </div>
      </details>
      ` + note
	}

	return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ccam invite</title>
<style>` + unifiedTokens + `
  body { display:flex; min-height:100vh; align-items:center; justify-content:center; padding:28px 20px; }
  .card { width:100%; max-width:560px; background:var(--raised); border:1px solid var(--line);
          border-radius:20px; padding:44px 44px 36px; box-shadow:0 24px 60px -24px rgba(0,0,0,.7);
          animation:rise .5s cubic-bezier(.2,.7,.3,1) both; }
  @keyframes rise { from { opacity:0; transform:translateY(14px); } to { opacity:1; transform:none; } }
  @media (prefers-reduced-motion: reduce) { .card { animation:none; } }

  h1 { margin:0 0 12px; font-size:30px; letter-spacing:-.025em; line-height:1.12; }
  .lead { margin:0 0 30px; color:var(--muted); font-size:17px; line-height:1.5; }

  .steps { list-style:none; margin:0 0 8px; padding:0; display:grid; gap:26px; }
  .steps li { display:flex; gap:16px; align-items:flex-start; min-width:0; }
  .steps .n { flex:none; width:30px; height:30px; border-radius:50%;
    background:linear-gradient(160deg, var(--primary), #8a97ff);
    color:var(--primary-ink); font-weight:800; font-size:15px;
    display:flex; align-items:center; justify-content:center; box-shadow:0 4px 14px -4px var(--primary); }
  .step-body { flex:1; min-width:0; }
  .steps b { display:block; font-size:17px; margin-bottom:3px; }
  .det { display:block; color:var(--muted); font-size:14.5px; line-height:1.5; }
  .det.small { font-size:13px; margin-top:10px; }
  .det a { color:var(--primary); text-decoration:none; }
  .det a:hover { text-decoration:underline; }
  .fieldlabel { display:block; font-size:12.5px; color:var(--faint); margin:16px 0 7px; }

  .os-tabs { display:inline-flex; gap:4px; margin:14px 0 8px; padding:3px; background:var(--sunken);
    border:1px solid var(--line); border-radius:9px; }
  .os-tab { background:none; border:none; color:var(--muted); font:inherit; font-size:13px; font-weight:500;
    padding:6px 12px; border-radius:6px; cursor:pointer; transition:all .15s ease; }
  .os-tab:hover { color:var(--ink); }
  .os-tab.is-on { background:var(--raised-2); color:var(--ink); }

  .copybox { display:flex; gap:8px; margin-top:2px; }
  .copybox code { flex:1; min-width:0; overflow-x:auto; white-space:nowrap; background:var(--sunken);
    border:1px solid var(--line); border-radius:10px; padding:13px 14px;
    font-family:var(--mono); font-size:13.5px; color:var(--ink); scrollbar-width:thin; }
  .copybox.big code { font-size:14px; padding:14px 15px; }
  .copybox .copy { flex:none; background:var(--raised-2); color:var(--ink); border:1px solid var(--line);
    border-radius:10px; padding:0 18px; font:inherit; font-size:14px; font-weight:600; cursor:pointer;
    transition:transform .1s ease, filter .15s ease, background .15s ease; }
  .copybox .copy:hover { background:#252c37; transform:translateY(-1px); }
  .copybox .copy:active { transform:translateY(0); }
  .copybox .copy.primary { background:var(--primary); color:var(--primary-ink); border-color:var(--primary); }
  .copybox .copy.primary:hover { filter:brightness(1.08); }
  .copybox .copy.copied { background:var(--ok); color:var(--primary-ink); border-color:var(--ok); }

  .terminal { margin-top:28px; padding-top:22px; border-top:1px solid var(--line); }
  .terminal summary { cursor:pointer; color:var(--muted); font-size:14px; font-weight:500;
    list-style:none; user-select:none; transition:color .15s ease; }
  .terminal summary:hover { color:var(--ink); }
  .terminal summary::before { content:"›"; display:inline-block; margin-right:8px; transition:transform .2s ease; }
  .terminal[open] summary::before { transform:rotate(90deg); }
  .terminal .det { margin:12px 0 8px; }

  .muted { color:var(--muted); } .small { font-size:13px; margin:16px 0 0; }
  @media (max-width:560px){ .card { padding:32px 24px 28px; } h1 { font-size:25px; } }
</style></head>
<body>
  <div class="card">` + body + `</div>
  <script>
    // One copy handler for every copybox button.
    document.querySelectorAll('.copy[data-copy-target]').forEach(function(btn){
      btn.addEventListener('click', function(){
        var el = document.getElementById(btn.getAttribute('data-copy-target'));
        if(!el) return;
        navigator.clipboard.writeText(el.textContent).then(function(){
          var t = btn.textContent; btn.textContent = 'Copied'; btn.classList.add('copied');
          setTimeout(function(){ btn.textContent = t; btn.classList.remove('copied'); }, 1500);
        }).catch(function(){ btn.textContent = 'Copy failed'; });
      });
    });
    // OS tabs swap the install command; default to the visitor's platform.
    (function(){
      var cmd = document.getElementById('install-cmd');
      if(!cmd) return;
      var cmds = { unix: ` + jsString(installUnixCmd) + `, win: ` + jsString(installWinCmd) + ` };
      var tabs = document.querySelectorAll('.os-tab');
      function pick(os){
        cmd.textContent = cmds[os] || cmds.unix;
        tabs.forEach(function(t){ t.classList.toggle('is-on', t.getAttribute('data-os')===os); });
      }
      tabs.forEach(function(t){ t.addEventListener('click', function(){ pick(t.getAttribute('data-os')); }); });
      var ua = (navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || navigator.userAgent || '';
      if(/win/i.test(ua)) pick('win');
    })();
  </script>
</body></html>`
}

// jsString renders a Go string as a safe JavaScript string literal.
func jsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "<", `\x3c`, ">", `\x3e`)
	return "'" + r.Replace(s) + "'"
}

// unifiedTokens is the one design system the panel, the local client page and
// this invite page are built from: the same palette, type and radius, so the
// three never look like separate products. It is CSS custom properties plus the
// couple of base rules every page needs, and nothing page-specific.
var unifiedTokens = strings.TrimSpace(`
  :root {
    color-scheme: dark;
    --ground:#0F1216; --raised:#171B21; --raised-2:#1E242D; --sunken:#0B0E12; --line:#262C34;
    --ink:#E7EBF0; --muted:#9AA4B2; --faint:#626D7C;
    --primary:#6E8BFF; --primary-ink:#0B0E12;
    --ok:#46C08A; --warn:#E0A83E; --crit:#E05C53;
    --radius:12px;
    --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Ubuntu, Helvetica, Arial, sans-serif;
    --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--ground); color:var(--ink);
         font-family:var(--font); font-size:16px; line-height:1.5;
         -webkit-font-smoothing:antialiased; }
`)
