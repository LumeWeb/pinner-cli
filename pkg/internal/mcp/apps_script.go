package mcp

// pinAppLogic is the "Create a Pin" app's app-specific ESM module code. It is
// prefixed by the shared ext-apps bootstrap (extAppsConnectSnippet) via
// mcpAppModule, which supplies `$`, `setStatus(el, state, msg)` and
// `extAppsConnect(clientB64, name, version)` and fills the __CLIENT_B64__
// placeholder. This file therefore authors ONLY the pin form's behavior: the
// submit handler, the terminal-status poll loop, and the result readout.
const pinAppLogic = `const CLIENT_B64 = "__CLIENT_B64__";

const statusEl = $("#pin-status");

extAppsConnect(CLIENT_B64, "CreatePin", "1.0.0").then((app) => {
  $("#pin-form").addEventListener("submit", (ev) => {
    ev.preventDefault();
    const cid = $("#cid").value.trim();
    const name = $("#name").value.trim();
    if (!cid) { setStatus(statusEl, "error", "A CID is required."); return; }
    setStatus(statusEl, "pending", "Pinning " + cid + " ...");
    app.callServerTool({ name: "pinner_pin", arguments: { cid, name } }).then((res) => {
      renderResult(app, res, cid);
      if (res && !res.isError) pollStatus(app, cid);
    });
  });
}).catch((err) => {
  setStatus(statusEl, "error", "Could not connect to host: " +
    (err && err.message ? err.message : err));
});

function renderResult(app, result, cid) {
  const sc = result && result.structuredContent;
  if (sc && sc.pin && sc.pin.cid) {
    $("#out-cid").textContent = sc.pin.cid;
  } else if (cid) {
    $("#out-cid").textContent = cid;
  }
  if (sc && sc.pin && sc.pin.status) {
    $("#out-status").textContent = sc.pin.status;
  }
  setStatus(statusEl, result && result.isError ? "error" : "ok",
    result && result.isError ? "Pin failed." : "Pin scheduled.");
}

function pollStatus(app, cid, attempts) {
  let max = attempts || 24;
  return app.callServerTool({
    name: "pinner_pin_status",
    arguments: { cid },
  }).then((res) => {
    const st = res && res.structuredContent && res.structuredContent.status;
    if (st) {
      $("#out-status").textContent = st;
    }
    // A terminal status wins even on the final allowed attempt.
    if (st === "pinned" || st === "failed" || st === "error") {
      setStatus(statusEl, st === "pinned" ? "ok" : "info",
        st === "pinned" ? "Pinned." : "Current status: " + st);
      return;
    }
    // Missing status (an IsError result, e.g. ErrPinNotFound right after a pin
    // is scheduled, or a transient failure) is not terminal: keep polling until
    // the attempt budget is exhausted rather than silently stopping the UI.
    if (--max <= 0) {
      setStatus(statusEl, "info", "Timed out polling pin status (last: " + (st || "unknown") + ").");
      return;
    }
    window.setTimeout(() => pollStatus(app, cid, max), 1500);
  }).catch(() => {
    // Transient transport/network error: retry until the budget is exhausted.
    if (--max > 0) {
      setStatus(statusEl, "pending", "Checking pin status...");
      window.setTimeout(() => pollStatus(app, cid, max), 1500);
    } else {
      setStatus(statusEl, "info", "Timed out polling pin status.");
    }
  });
}
`

// pinAppModule renders the "Create a Pin" app's ESM module source: the shared
// ext-apps bootstrap plus the pin form logic. It serves as the body of the
// <script type="module"> in ui://pins/create.html, injected by Go (via
// renderMcpAppDoc) — kept in a .go raw string so the JS braces and quotes never
// collide with templ's HTML expression parser.
func pinAppModule(clientBase64 string) string {
	return mcpAppModule(pinAppLogic, clientBase64)
}
