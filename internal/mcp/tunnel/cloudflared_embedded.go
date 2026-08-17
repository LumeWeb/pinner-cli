package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"runtime"
	"time"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/supervisor"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
)

// embeddedCloudflaredVersion is the version string reported to the Cloudflare
// edge when a connector from this library establishes its connection. It has
// no behavioral effect on the named tunnel itself; it is advisory telemetry.
const embeddedCloudflaredVersion = "2026.8.2-embedded"

// startTunnelDaemon is the seam used to launch the in-process cloudflared
// daemon. Production uses supervisor.StartTunnelDaemon; tests redirect it to
// observe the constructed configuration without touching the network.
var startTunnelDaemon = func(
	ctx context.Context,
	cfg *supervisor.TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	connectedSignal *signal.Signal,
	reconnectCh chan supervisor.ReconnectSignal,
	shutdown <-chan struct{},
) error {
	return supervisor.StartTunnelDaemon(ctx, cfg, orchestrator, connectedSignal, reconnectCh, shutdown)
}

// buildCloudflaredTunnelProperties maps the persisted CloudflareTunnelState
// onto the connection.TunnelProperties cloudflared needs to run a NAMED
// tunnel. QuickTunnelUrl is left empty, which is exactly the distinction
// between a named tunnel and a trycloudflared quick tunnel.
//
// state.Secret is stored base64 (see internal/cloudflare NewTunnelSecret,
// which base64.StdEncoding-encodes 32 random bytes). cloudflared's
// connection.Credentials.TunnelSecret holds the RAW secret bytes used for the
// edge-registration HMAC (the exec path previously wrote the base64 string to
// a credentials file, which cloudflared's loader then base64-decoded; the
// in-process struct must decode it here instead), so it is decoded before
// assignment.
func buildCloudflaredTunnelProperties(state *CloudflareTunnelState) (*connection.TunnelProperties, error) {
	if state == nil {
		return nil, fmt.Errorf("nil tunnel state")
	}
	if state.TunnelID == "" {
		return nil, fmt.Errorf("provisioned tunnel state is missing tunnel id")
	}
	if state.AccountID == "" {
		return nil, fmt.Errorf("provisioned tunnel state is missing account id")
	}
	tunnelID, err := uuid.Parse(state.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("provisioned tunnel id %q is not a valid UUID: %w", state.TunnelID, err)
	}
	rawSecret, err := base64.StdEncoding.DecodeString(state.Secret)
	if err != nil {
		return nil, fmt.Errorf("provisioned tunnel secret is not valid base64: %w", err)
	}
	return &connection.TunnelProperties{
		Credentials: connection.Credentials{
			AccountTag:   state.AccountID,
			TunnelSecret: rawSecret,
			TunnelID:     tunnelID,
			Endpoint:     "",
		},
		// QuickTunnelUrl empty => named tunnel.
	}, nil
}

// buildCloudflaredIngress parses the ingress table for an embedded named
// tunnel: the first rule routes the provisioned (bare) hostname to the local
// origin, and a catch-all returns 404 for any other host on the same tunnel.
// The bare hostname form matches what cloudflared's ingress parser expects.
func buildCloudflaredIngress(hostname, origin string) (ingress.Ingress, error) {
	return ingress.ParseIngress(&config.Configuration{
		Ingress: []config.UnvalidatedIngressRule{
			{Hostname: hostname, Service: origin},
			{Service: "http_status:404"},
		},
	})
}

// startEmbeddedCloudflared builds and launches an in-process cloudflared NAMED
// tunnel that routes state.Hostname to the given local origin. It blocks until
// the daemon exits (on ctx cancellation or its own failure).
func startEmbeddedCloudflared(ctx context.Context, state *CloudflareTunnelState, origin string) error {
	logTransport := logger.Create(logger.CreateConfig("", true, false, "", ""))

	observer := connection.NewObserver(logTransport, logTransport)

	featureSelector, err := features.NewFeatureSelector(ctx, state.AccountID, nil, false, logTransport)
	if err != nil {
		return fmt.Errorf("create feature selector: %w", err)
	}

	clientConfig, err := client.NewConfig(embeddedCloudflaredVersion, runtime.GOOS+"_"+runtime.GOARCH, featureSelector)
	if err != nil {
		return fmt.Errorf("create client config: %w", err)
	}

	host := BareHostname(state.Hostname)
	ing, err := buildCloudflaredIngress(host, origin)
	if err != nil {
		return fmt.Errorf("parse ingress: %w", err)
	}

	orchestrator, err := orchestration.NewOrchestrator(
		ctx,
		&orchestration.Config{
			Ingress:            &ing,
			WarpRouting:        ingress.NewWarpRoutingConfig(&config.WarpRoutingConfig{}),
			ConfigurationFlags: map[string]string{},
		},
		[]pogs.Tag{},
		[]ingress.Rule{},
		logTransport,
	)
	if err != nil {
		return fmt.Errorf("create orchestrator: %w", err)
	}

	connectedSignal := signal.New(make(chan struct{}))
	reconnectCh := make(chan supervisor.ReconnectSignal, 4)

	protocolSelector, err := connection.NewProtocolSelector(
		connection.HTTP2.String(),
		state.AccountID,
		false, // no --token flag; the tunnel runs on the persisted credentials
		edgediscovery.ProtocolPercentage,
		connection.ResolveTTL,
		logTransport,
	)
	if err != nil {
		return fmt.Errorf("create protocol selector: %w", err)
	}

	edgeTLSConfigs := make(map[connection.Protocol]*tls.Config, len(connection.ProtocolList))
	for _, p := range connection.ProtocolList {
		tlsSettings := p.TLSSettings()
		if tlsSettings == nil {
			return fmt.Errorf("%s has unknown TLS settings", p)
		}
		// No custom CA cert is supplied for an embedded child process, so pass
		// the empty default and let CreateTunnelConfig load the system roots.
		edgeTLSConfig, err := tlsconfig.CreateTunnelConfig("", tlsSettings.ServerName)
		if err != nil {
			return fmt.Errorf("create edge TLS config: %w", err)
		}
		if len(tlsSettings.NextProtos) > 0 {
			edgeTLSConfig.NextProtos = tlsSettings.NextProtos
		}
		edgeTLSConfigs[p] = edgeTLSConfig
	}

	namedTunnel, err := buildCloudflaredTunnelProperties(state)
	if err != nil {
		return err
	}

	tunnelConfig := &supervisor.TunnelConfig{
		ClientConfig:                        clientConfig,
		GracePeriod:                         30,
		EdgeAddrs:                           []string{},
		Region:                              "",
		EdgeIPVersion:                       allregions.Auto,
		EdgeBindAddr:                        nil,
		HAConnections:                       2,
		IsAutoupdated:                       false,
		LBPool:                              "",
		Tags:                                []pogs.Tag{},
		Log:                                 logTransport,
		LogTransport:                        logTransport,
		Observer:                            observer,
		ReportedVersion:                     embeddedCloudflaredVersion,
		Retries:                             5,
		RunFromTerminal:                     false,
		NamedTunnel:                         namedTunnel,
		ProtocolSelector:                    protocolSelector,
		EdgeTLSConfigs:                      edgeTLSConfigs,
		MaxEdgeAddrRetries:                  8,
		RPCTimeout:                          5 * time.Second,
		WriteStreamTimeout:                  time.Second * 0,
		DisableQUICPathMTUDiscovery:         false,
		QUICConnectionLevelFlowControlLimit: 30 * (1 << 20),
		QUICStreamLevelFlowControlLimit:     6 * (1 << 20),
		ICMPRouterServer:                    nil,
	}

	shutdown := make(chan struct{})
	return startTunnelDaemon(ctx, tunnelConfig, orchestrator, connectedSignal, reconnectCh, shutdown)
}
