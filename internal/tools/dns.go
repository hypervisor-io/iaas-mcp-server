package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// DNS tools, mirroring the iaas_dns_zone, iaas_dns_record_set, and
// iaas_dns_record resources. Zone -> record set -> record hierarchy. All writes
// are synchronous EXCEPT zone delete, which is async (poll to 404). Record sets
// and records have no SHOW route (the client resolves them by scanning the zone
// SHOW). VPC attach/detach are zone-level actions.

func init() {
	toolRegistrars = append(toolRegistrars, registerDNSTools)
}

// ── zone inputs / outputs ───────────────────────────────────────────────────

type CreateDnsZoneInput struct {
	Name        string   `json:"name" jsonschema:"DNS zone name (e.g. example.com)"`
	Description string   `json:"description,omitempty" jsonschema:"optional description"`
	VPCIDs      []string `json:"vpc_ids,omitempty" jsonschema:"optional VPC UUIDs to attach at create time"`
}

type GetDnsZoneInput struct {
	ID string `json:"id" jsonschema:"UUID of the DNS zone"`
}

type ListDnsZonesInput struct{}

type UpdateDnsZoneInput struct {
	ID          string `json:"id" jsonschema:"UUID of the DNS zone"`
	Description string `json:"description,omitempty" jsonschema:"new description"`
}

type DeleteDnsZoneInput struct {
	ID string `json:"id" jsonschema:"UUID of the DNS zone to delete"`
	Confirmation
}

type DnsZoneVpcInput struct {
	ZoneID string `json:"zone_id" jsonschema:"UUID of the DNS zone"`
	VPCID  string `json:"vpc_id" jsonschema:"UUID of the VPC"`
}

type DnsZoneResult struct {
	Zone map[string]any `json:"zone"`
}

type DnsZoneListResult struct {
	Zones []map[string]any `json:"zones"`
	Count int              `json:"count"`
}

// ── record set inputs / outputs ─────────────────────────────────────────────

type CreateDnsRecordSetInput struct {
	ZoneID        string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	Name          string `json:"name" jsonschema:"record set name (subdomain, or @ for apex)"`
	Type          string `json:"type" jsonschema:"record type (A, AAAA, CNAME, TXT, SRV, ...)"`
	RoutingPolicy string `json:"routing_policy" jsonschema:"simple, weighted, multivalue, or failover"`
	TTL           int    `json:"ttl" jsonschema:"time to live in seconds"`
}

type GetDnsRecordSetInput struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the record set"`
}

type UpdateDnsRecordSetInput struct {
	ZoneID        string  `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID   string  `json:"record_set_id" jsonschema:"UUID of the record set"`
	Name          *string `json:"name,omitempty" jsonschema:"new name"`
	RoutingPolicy *string `json:"routing_policy,omitempty" jsonschema:"new routing policy"`
	TTL           *int    `json:"ttl,omitempty" jsonschema:"new TTL"`
}

type DeleteDnsRecordSetInput struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the record set to delete"`
	Confirmation
}

type DnsRecordSetResult struct {
	RecordSet map[string]any `json:"record_set"`
}

// ── record inputs / outputs ─────────────────────────────────────────────────

type CreateDnsRecordInput struct {
	ZoneID       string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID  string `json:"record_set_id" jsonschema:"UUID of the parent record set"`
	Value        string `json:"value" jsonschema:"the record value (e.g. an IP or target)"`
	Weight       *int   `json:"weight,omitempty" jsonschema:"weight (for weighted routing)"`
	FailoverRole string `json:"failover_role,omitempty" jsonschema:"primary or secondary (for failover routing)"`
	Enabled      *bool  `json:"enabled,omitempty" jsonschema:"whether the record is enabled"`
}

type GetDnsRecordInput struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the parent record set"`
	RecordID    string `json:"record_id" jsonschema:"UUID of the record"`
}

type UpdateDnsRecordInput struct {
	ZoneID      string  `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID string  `json:"record_set_id" jsonschema:"UUID of the parent record set"`
	RecordID    string  `json:"record_id" jsonschema:"UUID of the record"`
	Value       *string `json:"value,omitempty" jsonschema:"new value"`
	Weight      *int    `json:"weight,omitempty" jsonschema:"new weight"`
	Enabled     *bool   `json:"enabled,omitempty" jsonschema:"enable or disable"`
}

type DeleteDnsRecordInput struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the parent DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the parent record set"`
	RecordID    string `json:"record_id" jsonschema:"UUID of the record to delete"`
	Confirmation
}

type DnsRecordResult struct {
	Record map[string]any `json:"record"`
}

// ── zone handlers ───────────────────────────────────────────────────────────

func createDnsZone(ctx context.Context, cl *client.Client, in CreateDnsZoneInput) (DnsZoneResult, error) {
	body := map[string]any{"name": in.Name}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if len(in.VPCIDs) > 0 {
		body["vpc_ids"] = in.VPCIDs
	}
	created, err := cl.CreateDnsZone(ctx, body)
	if err != nil {
		return DnsZoneResult{}, err
	}
	// Read back for status + authoritative attached vpcs.
	id, _ := created["id"].(string)
	if id == "" {
		return DnsZoneResult{Zone: created}, nil
	}
	obj, err := cl.GetDnsZone(ctx, id)
	if err != nil {
		return DnsZoneResult{}, err
	}
	return DnsZoneResult{Zone: obj}, nil
}

func getDnsZone(ctx context.Context, cl *client.Client, in GetDnsZoneInput) (DnsZoneResult, error) {
	obj, err := cl.GetDnsZone(ctx, in.ID)
	if err != nil {
		return DnsZoneResult{}, err
	}
	return DnsZoneResult{Zone: obj}, nil
}

func listDnsZones(ctx context.Context, cl *client.Client, _ ListDnsZonesInput) (DnsZoneListResult, error) {
	items, err := cl.ListDnsZones(ctx)
	if err != nil {
		return DnsZoneListResult{}, err
	}
	return DnsZoneListResult{Zones: items, Count: len(items)}, nil
}

func updateDnsZone(ctx context.Context, cl *client.Client, in UpdateDnsZoneInput) (DnsZoneResult, error) {
	obj, err := cl.UpdateDnsZone(ctx, in.ID, map[string]any{"description": in.Description})
	if err != nil {
		return DnsZoneResult{}, err
	}
	return DnsZoneResult{Zone: obj}, nil
}

func deleteDnsZone(ctx context.Context, cl *client.Client, in DeleteDnsZoneInput) (DeleteResult, error) {
	if err := cl.DeleteDnsZone(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	// Zone delete is async; converge by polling SHOW to 404.
	err := waitForGone(ctx, func() (map[string]any, error) { return cl.GetDnsZone(ctx, in.ID) }, defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("dns zone %s was not removed: %w", in.ID, err)
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func attachDnsZoneVpc(ctx context.Context, cl *client.Client, in DnsZoneVpcInput) (OKResult, error) {
	if err := cl.AttachDnsZoneVpc(ctx, in.ZoneID, in.VPCID); err != nil {
		return OKResult{}, err
	}
	return okResult("vpc attached to zone"), nil
}

func detachDnsZoneVpc(ctx context.Context, cl *client.Client, in DnsZoneVpcInput) (OKResult, error) {
	if err := cl.DetachDnsZoneVpc(ctx, in.ZoneID, in.VPCID); err != nil {
		return OKResult{}, err
	}
	return okResult("vpc detached from zone"), nil
}

// ── record set handlers ─────────────────────────────────────────────────────

func createDnsRecordSet(ctx context.Context, cl *client.Client, in CreateDnsRecordSetInput) (DnsRecordSetResult, error) {
	body := map[string]any{
		"name":           in.Name,
		"type":           in.Type,
		"routing_policy": in.RoutingPolicy,
		"ttl":            in.TTL,
	}
	obj, err := cl.CreateDnsRecordSet(ctx, in.ZoneID, body)
	if err != nil {
		return DnsRecordSetResult{}, err
	}
	return DnsRecordSetResult{RecordSet: obj}, nil
}

func getDnsRecordSet(ctx context.Context, cl *client.Client, in GetDnsRecordSetInput) (DnsRecordSetResult, error) {
	obj, err := cl.GetDnsRecordSet(ctx, in.ZoneID, in.RecordSetID)
	if err != nil {
		return DnsRecordSetResult{}, err
	}
	return DnsRecordSetResult{RecordSet: obj}, nil
}

func updateDnsRecordSet(ctx context.Context, cl *client.Client, in UpdateDnsRecordSetInput) (DnsRecordSetResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.RoutingPolicy != nil {
		body["routing_policy"] = *in.RoutingPolicy
	}
	if in.TTL != nil {
		body["ttl"] = *in.TTL
	}
	obj, err := cl.UpdateDnsRecordSet(ctx, in.ZoneID, in.RecordSetID, body)
	if err != nil {
		return DnsRecordSetResult{}, err
	}
	return DnsRecordSetResult{RecordSet: obj}, nil
}

func deleteDnsRecordSet(ctx context.Context, cl *client.Client, in DeleteDnsRecordSetInput) (DeleteResult, error) {
	if err := cl.DeleteDnsRecordSet(ctx, in.ZoneID, in.RecordSetID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RecordSetID, Deleted: true}, nil
}

// ── record handlers ─────────────────────────────────────────────────────────

func createDnsRecord(ctx context.Context, cl *client.Client, in CreateDnsRecordInput) (DnsRecordResult, error) {
	body := map[string]any{"value": in.Value}
	if in.Weight != nil {
		body["weight"] = *in.Weight
	}
	if in.FailoverRole != "" {
		body["failover_role"] = in.FailoverRole
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.CreateDnsRecord(ctx, in.ZoneID, in.RecordSetID, body)
	if err != nil {
		return DnsRecordResult{}, err
	}
	return DnsRecordResult{Record: obj}, nil
}

func getDnsRecord(ctx context.Context, cl *client.Client, in GetDnsRecordInput) (DnsRecordResult, error) {
	obj, err := cl.GetDnsRecord(ctx, in.ZoneID, in.RecordSetID, in.RecordID)
	if err != nil {
		return DnsRecordResult{}, err
	}
	return DnsRecordResult{Record: obj}, nil
}

func updateDnsRecord(ctx context.Context, cl *client.Client, in UpdateDnsRecordInput) (DnsRecordResult, error) {
	body := map[string]any{}
	if in.Value != nil {
		body["value"] = *in.Value
	}
	if in.Weight != nil {
		body["weight"] = *in.Weight
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.UpdateDnsRecord(ctx, in.ZoneID, in.RecordSetID, in.RecordID, body)
	if err != nil {
		return DnsRecordResult{}, err
	}
	return DnsRecordResult{Record: obj}, nil
}

func deleteDnsRecord(ctx context.Context, cl *client.Client, in DeleteDnsRecordInput) (DeleteResult, error) {
	if err := cl.DeleteDnsRecord(ctx, in.ZoneID, in.RecordSetID, in.RecordID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RecordID, Deleted: true}, nil
}

func registerDNSTools(s *mcp.Server, deps Deps) {
	// Zones.
	Register(s, deps, Spec{Name: "user.dns_zone.create", Description: "Create a DNS zone (optionally attaching VPCs)."}, createDnsZone)
	Register(s, deps, Spec{Name: "user.dns_zone.list", Description: "List all DNS zones owned by the caller."}, listDnsZones)
	Register(s, deps, Spec{Name: "user.dns_zone.get", Description: "Get a DNS zone by UUID (with record sets and attached VPCs)."}, getDnsZone)
	Register(s, deps, Spec{Name: "user.dns_zone.update", Description: "Update a DNS zone's description."}, updateDnsZone)
	Register(s, deps, Spec{
		Name:        "user.dns_zone.delete",
		Description: "Delete a DNS zone and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDnsZone)
	Register(s, deps, Spec{Name: "user.dns_zone.attach_vpc", Description: "Attach a VPC to a DNS zone."}, attachDnsZoneVpc)
	Register(s, deps, Spec{Name: "user.dns_zone.detach_vpc", Description: "Detach a VPC from a DNS zone."}, detachDnsZoneVpc)

	// Record sets.
	Register(s, deps, Spec{Name: "user.dns_record_set.create", Description: "Create a DNS record set in a zone."}, createDnsRecordSet)
	Register(s, deps, Spec{Name: "user.dns_record_set.get", Description: "Get a DNS record set by zone UUID and record-set UUID."}, getDnsRecordSet)
	Register(s, deps, Spec{Name: "user.dns_record_set.update", Description: "Update a DNS record set's name, routing policy, or TTL."}, updateDnsRecordSet)
	Register(s, deps, Spec{
		Name:        "user.dns_record_set.delete",
		Description: "Delete a DNS record set (cascades its records). DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDnsRecordSet)

	// Records.
	Register(s, deps, Spec{Name: "user.dns_record.create", Description: "Create a DNS record in a record set."}, createDnsRecord)
	Register(s, deps, Spec{Name: "user.dns_record.get", Description: "Get a DNS record by zone, record-set, and record UUID."}, getDnsRecord)
	Register(s, deps, Spec{Name: "user.dns_record.update", Description: "Update a DNS record's value, weight, or enabled state."}, updateDnsRecord)
	Register(s, deps, Spec{
		Name:        "user.dns_record.delete",
		Description: "Delete a single DNS record. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDnsRecord)
}
