package mcp

import (
	"fmt"
	"strings"
)

// htmlEscapeText escapes text for safe interpolation into the raw-HTML string
// builders in this file (createVaultProgressStart/restoreVaultProgressStart).
// templ fragments escape automatically; these partial-document shells do not.
func htmlEscapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(s)
}

// seedWords splits a recovery mnemonic (whitespace-separated words, some BIP39
// styles use a trailing check word) into its words for rendering as a grid of
// copyable chips. Declaratively rendering each word avoids an <ol> whose list
// numbers could get copied alongside the words if the human highlights.
func seedWords(mnemonic string) []string {
	return strings.Fields(mnemonic)
}

// Vault create/restore progress pages are STREAMED: the Sia browser approval
// can block for minutes, so consumePOST writes an opening shell, streams the
// approval/link/result/error fragments into #status as they become known, then
// writes a closing tail. These pages use the shared Pinner brand (brand.css,
// pinner.xyz logo) but cannot be a single templ component because the document
// is deliberately OPEN between the start and end halves — templ components must
// be balanced, so the opening shell and closing tail are string builders that
// share the brand markup, and only the dynamic fragments between them are templ
// components.
//
// The opening halves mirror brandLayout's head/logo exactly (same classes,
// title suffix, brand.css and logo URLs) so the streamed page is visually
// identical to the other OOB pages.

// createVaultProgressStart opens the create-progress page. It stops before
// </body></html> so the fragments streamed by consumePOST land INSIDE #status.
func createVaultProgressStart(profile string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Create Pinner Vault · Pinner</title>
<link rel="stylesheet" href="%s"/>
</head>
<body class="brand-body">
<a class="brand-mark" href="https://pinner.xyz" target="_blank" rel="noopener" aria-label="Pinner — pinner.xyz">
<img class="brand-logo" src="%s" alt="Pinner logo" width="36" height="36"/>
</a>
<div class="brand-card">
<h1 class="brand-card-title">Create Pinner Vault</h1>
<p class="brand-card-text">Profile: <strong>%s</strong></p>
<div id="status">Preparing your new vault…</div>
`, brandCSSURL(), brandLogoURL(), htmlEscapeText(profile))
}

// restoreVaultProgressStart opens the restore-progress page.
func restoreVaultProgressStart(profile string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Restoring Pinner Vault · Pinner</title>
<link rel="stylesheet" href="%s"/>
</head>
<body class="brand-body">
<a class="brand-mark" href="https://pinner.xyz" target="_blank" rel="noopener" aria-label="Pinner — pinner.xyz">
<img class="brand-logo" src="%s" alt="Pinner logo" width="36" height="36"/>
</a>
<div class="brand-card">
<h1 class="brand-card-title">Restoring Pinner Vault</h1>
<p class="brand-card-text">Profile: <strong>%s</strong></p>
<div id="status">Starting restore…</div>
`, brandCSSURL(), brandLogoURL(), htmlEscapeText(profile))
}

// progressPageEnd closes the document opened by the progress start helpers: the
// .card and #status containers, the on-host note, the PINNER foot, the
// double-submit guard script, and </body></html>. Must be streamed AFTER all
// fragments.
func progressPageEnd() string {
	return `<div class="brand-foot">This page reflects the on-host vault state. The recovery phrase never leaves your browser.</div>
</div>
<div class="brand-foot">This server runs tools in-process under your PINNER account.</div>
<script>
document.addEventListener("submit", function (e) {
	var f = e.target;
	if (!f || !f.matches("form")) return;
	var btn = f.querySelector("button[type=submit]");
	if (!btn) return;
	btn.disabled = true;
	btn.classList.add("processing");
	if (!btn.querySelector(".spin")) {
		var s = document.createElement("span");
		s.className = "spin";
		s.setAttribute("aria-hidden", "true");
		btn.prepend(s);
	}
});
</script>
</body>
</html>`
}
