package mcp

import "strings"

// pinAppModule renders the "Create a Pin" app's ESM module source, serving as
// the body of the <script type="module"> in ui://pins/create.html. It is
// emitted via templ.Raw so the sandboxed iframe needs no network request.
//
// The module loads the vendored @modelcontextprotocol/ext-apps client bundle
// (App + PostMessageTransport) from the base64 clientBase64, connects to the
// host over the message bridge, and wires the pin form to
// callServerTool("pinner_pin") and the app-only "pinner_pin_status" polling
// helper until the pin reaches a terminal state.
//
// Kept in a .go raw string (not inside the .templ file) so the JS braces and
// quotes never collide with templ's HTML expression parser.
func pinAppModule(clientBase64 string) string {
	return strings.ReplaceAll(pinAppModuleSrc, "__CLIENT_B64__", clientBase64)
}

const pinAppModuleSrc = `const CLIENT_B64 = "__CLIENT_B64__";
const clientSrc = atob(CLIENT_B64);
const mod = await import(URL.createObjectURL(
  new Blob([clientSrc], { type: "text/javascript" })));
const { App, PostMessageTransport } = mod;

const app = new App(
  { name: "CreatePin", version: "1.0.0" },
  {}, // capabilities
);

function $(sel) { return document.querySelector(sel); }

function setStatus(state, msg) {
  const el = $("#pin-status");
  el.className = "status " + state;
  el.textContent = msg;
}

function renderResult(result, cid) {
  const sc = result && result.structuredContent;
  if (sc && sc.pin && sc.pin.cid) {
    $("#out-cid").textContent = sc.pin.cid;
  } else if (cid) {
    $("#out-cid").textContent = cid;
  }
  if (sc && sc.pin && sc.pin.status) {
    $("#out-status").textContent = sc.pin.status;
  }
  setStatus(result && result.isError ? "error" : "ok",
    result && result.isError ? "Pin failed." : "Pin scheduled.");
}

function pollStatus(cid, attempts) {
  const max = attempts || 24;
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
      setStatus(st === "pinned" ? "ok" : "info",
        st === "pinned" ? "Pinned." : "Current status: " + st);
      return;
    }
    // Missing status (an IsError result, e.g. ErrPinNotFound right after a pin
    // is scheduled, or a transient failure) is not terminal: keep polling until
    // the attempt budget is exhausted rather than silently stopping the UI.
    if (--max <= 0) {
      setStatus("info", "Timed out polling pin status (last: " + (st || "unknown") + ").");
      return;
    }
    window.setTimeout(() => pollStatus(cid, max), 1500);
  }).catch(() => {
    // Transient transport/network error: retry until the budget is exhausted.
    if (--max > 0) {
      setStatus("pending", "Checking pin status...");
      window.setTimeout(() => pollStatus(cid, max), 1500);
    } else {
      setStatus("info", "Timed out polling pin status.");
    }
  });
}

$("#pin-form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  const cid = $("#cid").value.trim();
  const name = $("#name").value.trim();
  if (!cid) { setStatus("error", "A CID is required."); return; }
  setStatus("pending", "Pinning " + cid + " ...");
  app.callServerTool({ name: "pinner_pin", arguments: { cid, name } }).then((res) => {
    renderResult(res, cid);
    if (res && !res.isError) pollStatus(cid);
  });
});

const transport = new PostMessageTransport(window.parent, window.parent);
app.connect(transport).catch((err) => {
  setStatus("error", "Could not connect to host: " +
    (err && err.message ? err.message : err));
});
`
