package cli

import "strings"

// dnsGuidance maps known validation-failure signals to actionable CLI next
// steps. Keyed by substring to stay tolerant of server wording drift; empty
// means "no specific advice".
func dnsGuidance(msg string) []string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "dnssec") || strings.Contains(m, "signing key") ||
		strings.Contains(m, "no active signing"):
		return []string{
			"DNSSEC is not enabled for this domain.",
			"Re-run 'pinner websites domains verify <domain>' — the portal self-heals DNSSEC on verify.",
			"If 'dns-requirements' still reports a DNSSEC error, contact support with the DNSSEC Error above.",
		}
	case strings.Contains(m, "not live") || strings.Contains(m, "not yet") ||
		strings.Contains(m, "propagat"):
		return []string{
			"The parent NS/DS records have not propagated yet.",
			"Publish the parent records (see 'pinner websites domains dns-requirements <domain>' for exactly what to publish), then wait for propagation.",
			"Re-run 'pinner websites domains verify <domain>' to confirm.",
		}
	case strings.Contains(m, "resolver"):
		return []string{
			"The DNS resolver could not be reached.",
			"Check your network connection, then re-run verify.",
		}
	default:
		return nil
	}
}

// renderDNSSelfServiceGuidance prints actionable next steps alongside an error
// so a failed/indeterminate DNS validation tells the user what to do next, not
// just the reason. It no-ops when there is no guidance for the message.
func renderDNSSelfServiceGuidance(output Output, err error) {
	for _, line := range dnsGuidance(err.Error()) {
		output.Printfln("  %s", line)
	}
}
