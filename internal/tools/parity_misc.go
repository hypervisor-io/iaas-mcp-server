package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Misc parity action tools: alert acknowledge, notification test, ssh key
// generation, S3 object public-url, DNS record health-check/toggle, and task
// list/delete.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityMiscTools)
}

type GenerateSSHKeyInput struct {
	Name      string `json:"name" jsonschema:"name for the generated key (max 32 chars, [a-z0-9 .-])"`
	Algorithm string `json:"algorithm" jsonschema:"rsa or ed25519"`
	Bits      *int   `json:"bits,omitempty" jsonschema:"key size for rsa (256, 2048, 3072, or 4096)"`
}

// GeneratedSSHKeyResult carries the stored key AND the private key, which the
// server returns exactly once and never persists.
type GeneratedSSHKeyResult struct {
	SSHKey     map[string]any `json:"ssh_key"`
	PrivateKey string         `json:"private_key"`
}

type S3ObjectPublicURLInput struct {
	BucketID  string `json:"bucket_id" jsonschema:"UUID of the bucket"`
	ObjectKey string `json:"object_key" jsonschema:"key of the object (max 1024 chars)"`
}

type URLResult struct {
	URL string `json:"url"`
}

type DnsRecordHealthCheckInput struct {
	ZoneID         string `json:"zone_id" jsonschema:"UUID of the DNS zone"`
	RecordSetID    string `json:"record_set_id" jsonschema:"UUID of the record set"`
	RecordID       string `json:"record_id" jsonschema:"UUID of the record"`
	Type           string `json:"type" jsonschema:"http, https, tcp, or icmp"`
	Port           *int   `json:"port,omitempty" jsonschema:"port (1-65535)"`
	Path           string `json:"path,omitempty" jsonschema:"HTTP path"`
	ExpectedStatus *int   `json:"expected_status,omitempty" jsonschema:"expected HTTP status (100-599)"`
	Interval       *int   `json:"interval,omitempty" jsonschema:"check interval seconds (10-300)"`
	Timeout        *int   `json:"timeout,omitempty" jsonschema:"timeout seconds (2-60)"`
}

type DnsRecordRef struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the record set"`
	RecordID    string `json:"record_id" jsonschema:"UUID of the record"`
}

type DeleteDnsRecordHealthCheckInput struct {
	ZoneID      string `json:"zone_id" jsonschema:"UUID of the DNS zone"`
	RecordSetID string `json:"record_set_id" jsonschema:"UUID of the record set"`
	RecordID    string `json:"record_id" jsonschema:"UUID of the record"`
	Confirmation
}

type ListTasksInput struct{}

type DeleteTaskInput struct {
	ID string `json:"id" jsonschema:"UUID of the task record to delete"`
	Confirmation
}

func acknowledgeAlertRule(ctx context.Context, cl *client.Client, in GetAlertRuleInput) (OKResult, error) {
	if err := cl.AcknowledgeAlertRule(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("alert acknowledged"), nil
}

func testNotificationChannel(ctx context.Context, cl *client.Client, in GetNotificationChannelInput) (OKResult, error) {
	if err := cl.TestNotificationChannel(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("test notification sent"), nil
}

func generateSSHKey(ctx context.Context, cl *client.Client, in GenerateSSHKeyInput) (GeneratedSSHKeyResult, error) {
	body := map[string]any{"name": in.Name, "algorithm": in.Algorithm}
	if in.Bits != nil {
		body["bits"] = *in.Bits
	}
	obj, err := cl.GenerateSSHKey(ctx, body)
	if err != nil {
		return GeneratedSSHKeyResult{}, err
	}
	key, _ := obj["ssh_key"].(map[string]any)
	priv, _ := obj["private_key"].(string)
	return GeneratedSSHKeyResult{SSHKey: key, PrivateKey: priv}, nil
}

func s3ObjectPublicURL(ctx context.Context, cl *client.Client, in S3ObjectPublicURLInput) (URLResult, error) {
	obj, err := cl.GetS3BucketObjectPublicUrl(ctx, in.BucketID, map[string]any{"object_key": in.ObjectKey})
	if err != nil {
		return URLResult{}, err
	}
	url, _ := obj["url"].(string)
	return URLResult{URL: url}, nil
}

func setDnsRecordHealthCheck(ctx context.Context, cl *client.Client, in DnsRecordHealthCheckInput) (ObjectResult, error) {
	body := map[string]any{"type": in.Type}
	if in.Port != nil {
		body["port"] = *in.Port
	}
	if in.Path != "" {
		body["path"] = in.Path
	}
	if in.ExpectedStatus != nil {
		body["expected_status"] = *in.ExpectedStatus
	}
	if in.Interval != nil {
		body["interval"] = *in.Interval
	}
	if in.Timeout != nil {
		body["timeout"] = *in.Timeout
	}
	return objectResult(cl.SetDnsRecordHealthCheck(ctx, in.ZoneID, in.RecordSetID, in.RecordID, body))
}

func deleteDnsRecordHealthCheck(ctx context.Context, cl *client.Client, in DeleteDnsRecordHealthCheckInput) (OKResult, error) {
	if err := cl.DeleteDnsRecordHealthCheck(ctx, in.ZoneID, in.RecordSetID, in.RecordID); err != nil {
		return OKResult{}, err
	}
	return okResult("health check removed"), nil
}

func toggleDnsRecord(ctx context.Context, cl *client.Client, in DnsRecordRef) (ObjectResult, error) {
	return objectResult(cl.ToggleDnsRecord(ctx, in.ZoneID, in.RecordSetID, in.RecordID))
}

func listTasks(ctx context.Context, cl *client.Client, _ ListTasksInput) (ItemsResult, error) {
	return itemsResult(cl.ListTasks(ctx))
}

func deleteTask(ctx context.Context, cl *client.Client, in DeleteTaskInput) (DeleteResult, error) {
	if err := cl.DeleteTask(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerParityMiscTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.alert_rule.acknowledge", Description: "Acknowledge a firing alert rule."}, acknowledgeAlertRule)
	Register(s, deps, Spec{Name: "user.notification_channel.test", Description: "Send a test notification to a channel."}, testNotificationChannel)
	Register(s, deps, Spec{Name: "user.ssh_key.generate", Description: "Server-generate an SSH keypair; the private key is returned once."}, generateSSHKey)
	Register(s, deps, Spec{Name: "user.s3_bucket.object_public_url", Description: "Get a public URL for an object in a bucket."}, s3ObjectPublicURL)
	Register(s, deps, Spec{Name: "user.dns_record.set_health_check", Description: "Set a health check on a DNS record."}, setDnsRecordHealthCheck)
	Register(s, deps, Spec{
		Name:        "user.dns_record.delete_health_check",
		Description: "Remove a DNS record's health check. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDnsRecordHealthCheck)
	Register(s, deps, Spec{Name: "user.dns_record.toggle", Description: "Toggle a DNS record's enabled state."}, toggleDnsRecord)
	Register(s, deps, Spec{Name: "user.task.list", Description: "List platform tasks owned by the caller."}, listTasks)
	Register(s, deps, Spec{
		Name:        "user.task.delete",
		Description: "Delete a task record (does not cancel the underlying operation). DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteTask)
}
