package plugin

// Manifest is the immutable Plugin identity the binary presents during the IPC
// handshake. It must stay identical to plugin.yaml; OAP-01 conformance is
// asserted by the manifest test.
type Manifest struct {
	PluginID   string
	Version    string
	Extensions []string
}

// DefaultManifest is the single Plugin identity of this executable.
var DefaultManifest = Manifest{
	PluginID:   "urn:connectionagent:plugin:openamsm",
	Version:    "0.1.0",
	Extensions: []string{"event.ingress", "event.delivery"},
}
