import { entryBoot, boot } from "@/loader";
// Account email change MCP App — one-shot deep-link entrypoint.
//
// The account_email_change tool mints a one-time browser page where the human
// enters their new email + current password; the change runs synchronously in
// that page via authenticated UpdateEmail. There is nothing to poll, so this
// app uses the link (one-shot) mount: click -> mint -> show the page URL.
import { mountLinkApp } from "@/app-entry";
import type { LinkAppEntry } from "@/app-entry";

export const def: LinkAppEntry = {
  name: "AccountEmail",
  config: {
    startTool: "account_email_change",
    urlField: "action_url",
  },
  ids: { startBtn: "em-start", urlEl: "em-url", statusEl: "em-status" },
  copy: {
    startLabel: "Minting a one-time email change page...",
    openLabel: "Open email change page",
    startErrorMsg: "Could not start an email change.",
    noUrlMsg: "No email change page was returned.",
    alreadyDoneMsg: "You must sign in first (start sign-in, then retry).",
    doneMsg: "Open the page in a browser and enter your new email and current password.",
  },
};

export default entryBoot(def, mountLinkApp);
boot(entryBoot(def, mountLinkApp));
