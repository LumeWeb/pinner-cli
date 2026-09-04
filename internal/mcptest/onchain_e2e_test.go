package mcptest

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// TestConvertDomainToOnChainE2E drives the on-chain-managed conversion through
// the real ipfs-sdk client against the fake server: the happy path flips an
// HNS binding to onchain_managed (dns hosting dropped, delegation removed),
// the "already on-chain" 422 is reported as idempotent success returning the
// current state, and a non-HNS binding is refused as ineligible.
func TestConvertDomainToOnChainE2E(t *testing.T) {
	srv := New()
	ws := srv.IPFS().SeedWebsite("example.test", "QmYwAPJzv5CZsnAzt8auVZRnXbW7Z5k7pZNeRp4cQ3vJdH", "ipfs")
	tok := srv.Seed("e2e@example.com", "E2E", "Test")
	ts := srv.Start()
	defer ts.Close()

	client, err := ipfs.NewClient(ts.URL, tok)
	require.NoError(t, err, "construct ipfs-sdk client")
	websites := client.Websites()
	ctx := context.Background()
	websiteID := intToString(ws.Id)

	// Bind an HNS domain and convert it to on-chain managed.
	hns, err := websites.BindDomain(ctx, websiteID, ipfs.DomainRequest{
		Domain:    "acme",
		Namespace: "hns",
	})
	require.NoError(t, err, "bind hns domain via sdk against fake")

	converted, err := websites.ConvertDomainToOnChain(ctx, websiteID, intToString(hns.Id))
	require.NoError(t, err, "convert hns domain to on-chain managed via sdk against fake")
	require.NotNil(t, converted.Status)
	require.Equal(t, ipfs.DomainResponseStatusOnchainManaged, *converted.Status)
	require.False(t, converted.DnsHostingEnabled, "on-chain managed bindings drop DNS hosting")
	require.Nil(t, converted.Delegation, "on-chain managed bindings carry no delegation bundle")

	// The persisted state (not just the response) reflects the conversion.
	listed, err := websites.ListDomains(ctx, websiteID)
	require.NoError(t, err)
	for _, d := range listed {
		if d.Id == hns.Id {
			require.NotNil(t, d.Status)
			require.Equal(t, ipfs.DomainResponseStatusOnchainManaged, *d.Status)
			require.Nil(t, d.Delegation)
		}
	}

	// Re-convert hits "already on-chain managed" — the SDK reports that as
	// idempotent success with the current state.
	again, err := websites.ConvertDomainToOnChain(ctx, websiteID, intToString(hns.Id))
	require.NoError(t, err, "converting an already on-chain managed domain is idempotent success")
	require.NotNil(t, again.Status)
	require.Equal(t, ipfs.DomainResponseStatusOnchainManaged, *again.Status)

	// A non-HNS binding is ineligible: conversion is refused.
	icann, err := websites.BindDomain(ctx, websiteID, ipfs.DomainRequest{
		Domain:    "other.example",
		Namespace: "icann",
	})
	require.NoError(t, err, "bind icann domain via sdk against fake")

	// The SDK maps every 422 on this op to one generic refusal message; the
	// distinguishing details stay in the response body.
	_, err = websites.ConvertDomainToOnChain(ctx, websiteID, intToString(icann.Id))
	require.Error(t, err, "converting a non-HNS domain must be refused")
	require.Contains(t, err.Error(), "not eligible for on-chain conversion")
}

// intToString keeps the call sites readable (path params are strings).
func intToString(i int) string {
	return strconv.Itoa(i)
}
