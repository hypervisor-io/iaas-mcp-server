package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// VPN tools, mirroring the iaas_vpn_gateway, iaas_vpn_peer, and
// iaas_vpn_peering resources. Gateway create is nested under the VPC and is
// async (poll status to "active"); all other ops use the flat /vpn-gateway/{id}
// path. Peers and peerings have no SHOW/LIST route (resolved by scanning the
// gateway). There is no gateway list endpoint, so no list tool is exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerVPNTools)
}

// ── gateway inputs / outputs ────────────────────────────────────────────────

type CreateVpnGatewayInput struct {
	VPCID        string `json:"vpc_id" jsonschema:"UUID of the VPC to create the gateway in"`
	VpngwPlanID  string `json:"vpngw_plan_id" jsonschema:"UUID of the VPN gateway plan"`
	VPCSubnetID  string `json:"vpc_subnet_id" jsonschema:"UUID of the VPC subnet"`
	Name         string `json:"name,omitempty" jsonschema:"optional name"`
	TunnelSubnet string `json:"tunnel_subnet,omitempty" jsonschema:"optional tunnel subnet CIDR"`
	ListenPort   *int   `json:"listen_port,omitempty" jsonschema:"optional WireGuard listen port"`
}

type GetVpnGatewayInput struct {
	ID string `json:"id" jsonschema:"UUID of the VPN gateway"`
}

type DeleteVpnGatewayInput struct {
	ID string `json:"id" jsonschema:"UUID of the VPN gateway to delete"`
	Confirmation
}

type VpnGatewayResult struct {
	Gateway map[string]any `json:"gateway"`
}

// ── peer inputs / outputs ───────────────────────────────────────────────────

type AddVpnPeerInput struct {
	GatewayID    string   `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	Type         string   `json:"type" jsonschema:"road_warrior or site_to_site"`
	Name         string   `json:"name,omitempty" jsonschema:"optional peer name"`
	PublicKey    string   `json:"public_key,omitempty" jsonschema:"peer public key (site_to_site)"`
	Endpoint     string   `json:"endpoint,omitempty" jsonschema:"peer endpoint host:port (site_to_site)"`
	TunnelIP     string   `json:"tunnel_ip,omitempty" jsonschema:"peer tunnel IP"`
	AllowedIPs   []string `json:"allowed_ips,omitempty" jsonschema:"CIDRs routed to this peer"`
	DNS          string   `json:"dns,omitempty" jsonschema:"DNS for a road_warrior peer"`
	Keepalive    *int     `json:"keepalive,omitempty" jsonschema:"persistent keepalive seconds"`
	Enabled      *bool    `json:"enabled,omitempty" jsonschema:"whether the peer is enabled"`
	PresharedKey string   `json:"preshared_key,omitempty" jsonschema:"optional WireGuard preshared key"`
}

type GetVpnPeerInput struct {
	GatewayID string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	PeerID    string `json:"peer_id" jsonschema:"UUID of the peer"`
}

type UpdateVpnPeerInput struct {
	GatewayID  string   `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	PeerID     string   `json:"peer_id" jsonschema:"UUID of the peer"`
	Name       *string  `json:"name,omitempty" jsonschema:"new name"`
	PublicKey  *string  `json:"public_key,omitempty" jsonschema:"new public key"`
	Endpoint   *string  `json:"endpoint,omitempty" jsonschema:"new endpoint"`
	Keepalive  *int     `json:"keepalive,omitempty" jsonschema:"new keepalive seconds"`
	Enabled    *bool    `json:"enabled,omitempty" jsonschema:"enable or disable"`
	AllowedIPs []string `json:"allowed_ips,omitempty" jsonschema:"new allowed IPs"`
}

type RemoveVpnPeerInput struct {
	GatewayID string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	PeerID    string `json:"peer_id" jsonschema:"UUID of the peer to remove"`
	Confirmation
}

type VpnPeerResult struct {
	Peer map[string]any `json:"peer"`
}

type VpnPeerConfigResult struct {
	Config string `json:"config"`
}

// ── peering inputs / outputs ────────────────────────────────────────────────

type CreateVpnPeeringInput struct {
	GatewayID       string `json:"gateway_id" jsonschema:"UUID of the local VPN gateway"`
	RemoteGatewayID string `json:"remote_gateway_id" jsonschema:"UUID of the remote VPN gateway to peer with"`
}

type GetVpnPeeringInput struct {
	GatewayID string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	PeeringID string `json:"peering_id" jsonschema:"UUID of the peering"`
}

type DeleteVpnPeeringInput struct {
	GatewayID string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	PeeringID string `json:"peering_id" jsonschema:"UUID of the peering to delete"`
	Confirmation
}

type VpnPeeringResult struct {
	Peering map[string]any `json:"peering"`
}

// ── gateway handlers ────────────────────────────────────────────────────────

func createVpnGateway(ctx context.Context, cl *client.Client, in CreateVpnGatewayInput) (VpnGatewayResult, error) {
	body := map[string]any{"vpngw_plan_id": in.VpngwPlanID, "vpc_subnet_id": in.VPCSubnetID}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.TunnelSubnet != "" {
		body["tunnel_subnet"] = in.TunnelSubnet
	}
	if in.ListenPort != nil {
		body["listen_port"] = *in.ListenPort
	}
	created, err := cl.CreateVpnGateway(ctx, in.VPCID, body)
	if err != nil {
		return VpnGatewayResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return VpnGatewayResult{}, fmt.Errorf("create response did not include a gateway id")
	}
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetVpnGateway(ctx, id) },
		"status", []string{"active"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return VpnGatewayResult{}, fmt.Errorf("vpn gateway %s did not become active: %w", id, err)
	}
	obj, err := cl.GetVpnGateway(ctx, id)
	if err != nil {
		return VpnGatewayResult{}, err
	}
	return VpnGatewayResult{Gateway: obj}, nil
}

func getVpnGateway(ctx context.Context, cl *client.Client, in GetVpnGatewayInput) (VpnGatewayResult, error) {
	obj, err := cl.GetVpnGateway(ctx, in.ID)
	if err != nil {
		return VpnGatewayResult{}, err
	}
	return VpnGatewayResult{Gateway: obj}, nil
}

func deleteVpnGateway(ctx context.Context, cl *client.Client, in DeleteVpnGatewayInput) (DeleteResult, error) {
	if err := cl.DeleteVpnGateway(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// ── peer handlers ───────────────────────────────────────────────────────────

func addVpnPeer(ctx context.Context, cl *client.Client, in AddVpnPeerInput) (VpnPeerResult, error) {
	body := map[string]any{"type": in.Type}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.PublicKey != "" {
		body["public_key"] = in.PublicKey
	}
	if in.Endpoint != "" {
		body["endpoint"] = in.Endpoint
	}
	if in.TunnelIP != "" {
		body["tunnel_ip"] = in.TunnelIP
	}
	if in.AllowedIPs != nil {
		body["allowed_ips"] = in.AllowedIPs
	}
	if in.DNS != "" {
		body["dns"] = in.DNS
	}
	if in.Keepalive != nil {
		body["keepalive"] = *in.Keepalive
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.PresharedKey != "" {
		body["preshared_key"] = in.PresharedKey
	}
	obj, err := cl.AddVpnPeer(ctx, in.GatewayID, body)
	if err != nil {
		return VpnPeerResult{}, err
	}
	return VpnPeerResult{Peer: obj}, nil
}

func getVpnPeer(ctx context.Context, cl *client.Client, in GetVpnPeerInput) (VpnPeerResult, error) {
	obj, err := cl.GetVpnPeer(ctx, in.GatewayID, in.PeerID)
	if err != nil {
		return VpnPeerResult{}, err
	}
	return VpnPeerResult{Peer: obj}, nil
}

func updateVpnPeer(ctx context.Context, cl *client.Client, in UpdateVpnPeerInput) (VpnPeerResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.PublicKey != nil {
		body["public_key"] = *in.PublicKey
	}
	if in.Endpoint != nil {
		body["endpoint"] = *in.Endpoint
	}
	if in.Keepalive != nil {
		body["keepalive"] = *in.Keepalive
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.AllowedIPs != nil {
		body["allowed_ips"] = in.AllowedIPs
	}
	obj, err := cl.UpdateVpnPeer(ctx, in.GatewayID, in.PeerID, body)
	if err != nil {
		return VpnPeerResult{}, err
	}
	return VpnPeerResult{Peer: obj}, nil
}

func removeVpnPeer(ctx context.Context, cl *client.Client, in RemoveVpnPeerInput) (OKResult, error) {
	if err := cl.RemoveVpnPeer(ctx, in.GatewayID, in.PeerID); err != nil {
		return OKResult{}, err
	}
	return okResult("peer removed"), nil
}

func getVpnPeerConfig(ctx context.Context, cl *client.Client, in GetVpnPeerInput) (VpnPeerConfigResult, error) {
	cfg, err := cl.DownloadVpnPeerConfig(ctx, in.GatewayID, in.PeerID)
	if err != nil {
		return VpnPeerConfigResult{}, err
	}
	return VpnPeerConfigResult{Config: cfg}, nil
}

// ── peering handlers ────────────────────────────────────────────────────────

func createVpnPeering(ctx context.Context, cl *client.Client, in CreateVpnPeeringInput) (VpnPeeringResult, error) {
	obj, err := cl.CreateVpnPeering(ctx, in.GatewayID, in.RemoteGatewayID)
	if err != nil {
		return VpnPeeringResult{}, err
	}
	return VpnPeeringResult{Peering: obj}, nil
}

func getVpnPeering(ctx context.Context, cl *client.Client, in GetVpnPeeringInput) (VpnPeeringResult, error) {
	obj, err := cl.GetVpnPeering(ctx, in.GatewayID, in.PeeringID)
	if err != nil {
		return VpnPeeringResult{}, err
	}
	return VpnPeeringResult{Peering: obj}, nil
}

func deleteVpnPeering(ctx context.Context, cl *client.Client, in DeleteVpnPeeringInput) (DeleteResult, error) {
	if err := cl.DeleteVpnPeering(ctx, in.GatewayID, in.PeeringID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.PeeringID, Deleted: true}, nil
}

func registerVPNTools(s *mcp.Server, deps Deps) {
	// Gateway.
	Register(s, deps, Spec{
		Name:        "user.vpn_gateway.create",
		Description: "Create a VPN gateway in a VPC and wait until it is active.",
	}, createVpnGateway)
	Register(s, deps, Spec{Name: "user.vpn_gateway.get", Description: "Get a VPN gateway by UUID (with its peers)."}, getVpnGateway)
	Register(s, deps, Spec{
		Name:        "user.vpn_gateway.delete",
		Description: "Delete a VPN gateway. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVpnGateway)

	// Peers.
	Register(s, deps, Spec{Name: "user.vpn_gateway.add_peer", Description: "Add a peer (road_warrior or site_to_site) to a VPN gateway."}, addVpnPeer)
	Register(s, deps, Spec{Name: "user.vpn_gateway.get_peer", Description: "Get a VPN peer by gateway and peer UUID."}, getVpnPeer)
	Register(s, deps, Spec{Name: "user.vpn_gateway.update_peer", Description: "Update a VPN peer's fields."}, updateVpnPeer)
	Register(s, deps, Spec{
		Name:        "user.vpn_gateway.remove_peer",
		Description: "Remove a peer from a VPN gateway. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, removeVpnPeer)
	Register(s, deps, Spec{Name: "user.vpn_gateway.peer_config", Description: "Download a road_warrior peer's WireGuard config."}, getVpnPeerConfig)

	// Peerings.
	Register(s, deps, Spec{Name: "user.vpn_peering.create", Description: "Create a VPC-to-VPC VPN peering between two of your gateways."}, createVpnPeering)
	Register(s, deps, Spec{Name: "user.vpn_peering.get", Description: "Get a VPN peering by gateway and peering UUID."}, getVpnPeering)
	Register(s, deps, Spec{
		Name:        "user.vpn_peering.delete",
		Description: "Delete a VPN peering (removes the local side). DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVpnPeering)
}
