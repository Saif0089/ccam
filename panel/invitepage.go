package panel

import (
	"html"
	"strings"
)

// invitePageHTML is the page an invite link opens in a browser. It is a single
// self-contained document with no scripts beyond a copy button and no requests
// back to the panel: an invite is often opened by someone who has never seen
// ccam, so it has to explain itself and stand on its own.
//
// It shares the panel's colour tokens and type so the whole product looks like
// one thing — the invariant behind "unified design".
func invitePageHTML(link, state string) string {
	esc := html.EscapeString(link)

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
      <p class="lead">You'll get to run Claude Code on a shared account — no sign-in of your own, nothing to keep. Two steps:</p>

      <ol class="steps">
        <li><span class="n">1</span>
          <div><b>Install ccam</b><span class="det">Once per computer. Grab it from your admin, then it runs in the background.</span></div></li>
        <li><span class="n">2</span>
          <div><b>Connect this link</b><span class="det">Open the ccam page on your computer, choose <b>Connect</b>, and paste the link below. That's it — shared accounts show up on their own.</span></div></li>
      </ol>

      <label class="fieldlabel">Your invite link</label>
      <div class="linkrow">
        <code id="lnk">` + esc + `</code>
        <button id="copy" type="button">Copy</button>
      </div>
      <p class="muted small">Prefer the terminal? Run <code class="inline">ccam join ` + esc + `</code></p>
      ` + note
	}

	return `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ccam invite</title>
<style>` + unifiedTokens + `
  body { display:flex; min-height:100vh; align-items:center; justify-content:center; padding:24px; }
  .card { width:100%; max-width:520px; background:var(--raised); border:1px solid var(--line);
          border-radius:16px; padding:38px 36px 34px; }
  h1 { margin:0 0 10px; font-size:26px; letter-spacing:-.02em; line-height:1.15; }
  .lead { margin:0 0 26px; color:var(--muted); font-size:16px; }
  .steps { list-style:none; margin:0 0 28px; padding:0; display:grid; gap:18px; }
  .steps li { display:flex; gap:14px; align-items:flex-start; }
  .steps .n { flex:none; width:26px; height:26px; border-radius:50%; background:var(--primary);
              color:var(--primary-ink); font-weight:700; font-size:14px;
              display:flex; align-items:center; justify-content:center; }
  .steps b { display:block; font-size:15.5px; }
  .steps .det { display:block; color:var(--muted); font-size:14px; margin-top:2px; }
  .fieldlabel { display:block; font-size:12.5px; color:var(--faint); margin-bottom:8px;
                text-transform:none; }
  .linkrow { display:flex; gap:8px; }
  .linkrow code { flex:1; min-width:0; overflow-x:auto; white-space:nowrap; background:var(--sunken);
                  border:1px solid var(--line); border-radius:8px; padding:11px 12px;
                  font-family:var(--mono); font-size:13px; color:var(--ink); }
  .linkrow button { flex:none; background:var(--primary); color:var(--primary-ink); border:none;
                    border-radius:8px; padding:0 16px; font:inherit; font-weight:600; cursor:pointer; }
  .linkrow button:hover { filter:brightness(1.08); }
  code.inline { font-family:var(--mono); font-size:13px; color:var(--ink);
                background:var(--sunken); border:1px solid var(--line); border-radius:5px; padding:1px 6px; }
  .muted { color:var(--muted); } .small { font-size:13px; }
  .small { margin:16px 0 0; }
</style></head>
<body>
  <div class="card">` + body + `</div>
  <script>
    var b=document.getElementById('copy');
    if(b){b.addEventListener('click',function(){
      navigator.clipboard.writeText(document.getElementById('lnk').textContent).then(function(){
        var t=b.textContent; b.textContent='Copied'; setTimeout(function(){b.textContent=t;},1500);
      }).catch(function(){b.textContent='Copy failed';});
    });}
  </script>
</body></html>`
}

// unifiedTokens is the one design system both the panel and the local client
// page are built from: the same palette, type and radius, so the two never look
// like two products. It is CSS custom properties plus the couple of base rules
// every page needs, and nothing page-specific.
var unifiedTokens = strings.TrimSpace(`
  :root {
    color-scheme: dark;
    --ground:#0F1216; --raised:#171B21; --sunken:#0B0E12; --line:#262C34;
    --ink:#E7EBF0; --muted:#9AA4B2; --faint:#626D7C;
    --primary:#6E8BFF; --primary-ink:#0B0E12;
    --ok:#46C08A; --warn:#E0A83E; --crit:#E05C53;
    --radius:10px;
    --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Ubuntu, Helvetica, Arial, sans-serif;
    --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
  }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--ground); color:var(--ink);
         font-family:var(--font); font-size:16px; line-height:1.5;
         -webkit-font-smoothing:antialiased; }
`)
