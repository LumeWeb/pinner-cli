import { entryBoot, boot } from "@/loader";
// Account password change MCP App — one-shot deep-link entrypoint.
//
// The account_password_update tool mints a one-time browser page where the
// human enters their current + new password; the change runs synchronously in
// that page via authenticated UpdatePassword. There is nothing to poll, so this
// app uses the link (one-shot) mount: click -> mint -> show the page URL.
import { mountLinkApp } from "@/app-entry";
import type { LinkAppEntry } from "@/app-entry";

export const def: LinkAppEntry = {
  name: "AccountPassword",
  config: {
    startTool: "account_password_update",
    urlField: "action_url",
  },
  ids: { startBtn: "pw-start", urlEl: "pw-url", statusEl: "pw-status" },
  copy: {
    startLabel: "Minting a one-time password change page...",
    openLabel: "Open password change page",
    startErrorMsg: "Could not start a password change.",
    noUrlMsg: "No password change page was returned.",
    alreadyDoneMsg: "You must sign in first (start sign-in, then retry).",
    doneMsg: "Open the page in a browser and enter your current and new password.",
  },
};

export default entryBoot(def, mountLinkApp);
boot(entryBoot(def, mountLinkApp));
