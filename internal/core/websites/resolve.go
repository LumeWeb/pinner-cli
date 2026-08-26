package websites

import (
	"context"
	"fmt"
	"strconv"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-sdk/dnsname"
	"golang.org/x/sync/errgroup"
)

// ResolveWebsiteID resolves an ID-or-domain argument to a numeric website ID
// string. If the arg parses as a number it is returned as-is; otherwise the
// service lists websites and matches the domain (case-insensitively,
// DNS-normalized).
func ResolveWebsiteID(ctx context.Context, svc Service, arg string) (string, error) {
	if _, err := strconv.Atoi(arg); err == nil {
		return arg, nil
	}
	item, ok, err := catalog.ScanPages(ctx, svc,
		func(w ipfs.WebsiteItem) (bool, error) {
			return dnsname.Equal(w.Domain, arg), nil
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to look up website by domain: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("website not found for domain %q", arg)
	}
	return strconv.Itoa(item.Id), nil
}

// ResolveDomainID resolves a domain name-or-ID argument to a numeric domain
// binding ID within a website. Matches by name first (case-insensitive,
// tolerant of trailing dot), then by numeric binding ID.
func ResolveDomainID(ctx context.Context, svc Service, websiteID string, domainArg string) (string, error) {
	domains, err := svc.ListDomains(ctx, websiteID)
	if err != nil {
		return "", fmt.Errorf("failed to look up domain: %w", err)
	}
	for _, d := range domains {
		if dnsname.Equal(d.Domain, domainArg) {
			return strconv.Itoa(d.Id), nil
		}
	}
	for _, d := range domains {
		if strconv.Itoa(d.Id) == domainArg {
			return domainArg, nil
		}
	}
	return "", fmt.Errorf("domain %q not found for website %s", domainArg, websiteID)
}

// ResolveDomainBinding resolves a domain name-or-ID argument to its owning
// website ID + domain binding ID by scanning ALL websites. Name matching wins
// over numeric-ID matching (protects namespaces like HNS where a domain name
// can itself be numeric, e.g. "123"). A deferred listing error on an unrelated
// website blocks the numeric-ID fallback but does not abort a clean name match.
//
// The per-website bindings are fetched concurrently (bounded) rather than
// sequentially, avoiding an N+1 round-trip pattern: one List call plus
// parallel ListDomains calls instead of one List plus N serial ListDomains.
func ResolveDomainBinding(ctx context.Context, svc Service, domainArg string) (websiteID string, domainID string, err error) {
	websites, err := svc.List(ctx, ListOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to list websites: %w", err)
	}

	// Fetch each website's bound domains concurrently. Results are kept
	// index-aligned with the List order so the post-scan behaves identically
	// to a sequential walk: name match wins, then first numeric-ID match.
	type websiteDomains struct {
		wID     string
		domains []ipfs.DomainResponse
		err     error
	}
	results := make([]websiteDomains, len(websites))
	g, gctx := errgroup.WithContext(ctx)
	// Bound the fan-out so a large account doesn't open M simultaneous
	// requests; the common case (a handful of websites) stays fully parallel.
	g.SetLimit(16)
	for i, w := range websites {
		i, w := i, w
		g.Go(func() error {
			wID := strconv.Itoa(w.Id)
			domains, lerr := svc.ListDomains(gctx, wID)
			results[i] = websiteDomains{wID: wID, domains: domains, err: lerr}
			return nil
		})
	}
	_ = g.Wait() // per-website errors are captured in results, not propagated

	var idMatchWebsite, idMatchDomain string
	var deferredErr error

	for _, r := range results {
		if r.err != nil {
			if deferredErr == nil {
				deferredErr = fmt.Errorf("failed to look up domain on website %s: %w", r.wID, r.err)
			}
			continue
		}
		for _, d := range r.domains {
			if dnsname.Equal(d.Domain, domainArg) {
				return r.wID, strconv.Itoa(d.Id), nil
			}
			if idMatchDomain == "" && strconv.Itoa(d.Id) == domainArg {
				idMatchWebsite, idMatchDomain = r.wID, strconv.Itoa(d.Id)
			}
		}
	}

	if idMatchDomain != "" && deferredErr == nil {
		return idMatchWebsite, idMatchDomain, nil
	}
	if deferredErr != nil {
		return "", "", deferredErr
	}
	return "", "", fmt.Errorf("domain %q not found bound to any website", domainArg)
}
