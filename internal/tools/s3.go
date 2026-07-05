package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// S3 object-storage tools, mirroring the iaas_s3_bucket and iaas_s3_access_key
// resources. All synchronous. Bucket create returns no id, so it is read back
// by name. Access-key create returns the secret exactly once (captured here).
// The user API has no access-key delete route, so no delete tool is exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerS3Tools)
}

// ── bucket inputs / outputs ─────────────────────────────────────────────────

type CreateS3BucketInput struct {
	Name       string `json:"name" jsonschema:"bucket name"`
	S3PlanID   string `json:"s3_plan_id" jsonschema:"UUID of the S3 plan"`
	S3ServerID string `json:"s3_server_id" jsonschema:"UUID of the S3 server"`
}

type GetS3BucketInput struct {
	ID string `json:"id" jsonschema:"UUID of the bucket"`
}

type ListS3BucketsInput struct{}

type S3BucketACLInput struct {
	ID     string `json:"id" jsonschema:"UUID of the bucket"`
	Action string `json:"action" jsonschema:"public, private, upload, or download"`
}

type DeleteS3BucketInput struct {
	ID string `json:"id" jsonschema:"UUID of the bucket to delete"`
	Confirmation
}

type S3BucketKeyInput struct {
	BucketID   string `json:"bucket_id" jsonschema:"UUID of the bucket"`
	KeyID      string `json:"key_id" jsonschema:"UUID of the access key"`
	Permission string `json:"permission,omitempty" jsonschema:"read, write, or readwrite (required for attach/update)"`
}

type ListS3BucketKeysInput struct {
	BucketID string `json:"bucket_id" jsonschema:"UUID of the bucket"`
}

type S3BucketResult struct {
	Bucket    map[string]any `json:"bucket"`
	AccessKey string         `json:"access_key,omitempty"`
	SecretKey string         `json:"secret_key,omitempty"`
	Endpoint  string         `json:"endpoint,omitempty"`
}

type S3BucketListResult struct {
	Buckets []map[string]any `json:"buckets"`
	Count   int              `json:"count"`
}

type S3BucketKeyListResult struct {
	Keys  []map[string]any `json:"keys"`
	Count int              `json:"count"`
}

// ── access key inputs / outputs ─────────────────────────────────────────────

type CreateS3AccessKeyInput struct {
	Name string `json:"name" jsonschema:"access key name"`
}

type GetS3AccessKeyInput struct {
	ID string `json:"id" jsonschema:"UUID of the access key"`
}

type ListS3AccessKeysInput struct{}

type UpdateS3AccessKeyInput struct {
	ID     string  `json:"id" jsonschema:"UUID of the access key"`
	Name   *string `json:"name,omitempty" jsonschema:"new name"`
	Active *bool   `json:"active,omitempty" jsonschema:"enable or disable the key"`
}

type S3AccessKeyResult struct {
	AccessKey map[string]any `json:"access_key"`
	// Secret is populated ONLY on create (the server returns it once).
	Secret string `json:"secret_key,omitempty"`
}

type S3AccessKeyListResult struct {
	AccessKeys []map[string]any `json:"access_keys"`
	Count      int              `json:"count"`
}

// ── bucket handlers ─────────────────────────────────────────────────────────

func createS3Bucket(ctx context.Context, cl *client.Client, in CreateS3BucketInput) (S3BucketResult, error) {
	body := map[string]any{"name": in.Name, "s3_plan_id": in.S3PlanID, "s3_server_id": in.S3ServerID}
	if _, err := cl.CreateS3Bucket(ctx, body); err != nil {
		return S3BucketResult{}, err
	}
	// Create returns no id/body; read the bucket back by its unique name.
	obj, err := cl.GetS3BucketByName(ctx, in.Name)
	if err != nil {
		return S3BucketResult{}, err
	}
	return S3BucketResult{Bucket: obj}, nil
}

func getS3Bucket(ctx context.Context, cl *client.Client, in GetS3BucketInput) (S3BucketResult, error) {
	env, err := cl.GetS3Bucket(ctx, in.ID)
	if err != nil {
		return S3BucketResult{}, err
	}
	res := S3BucketResult{}
	if b, ok := env["bucket"].(map[string]any); ok {
		res.Bucket = b
	}
	res.AccessKey, _ = env["access_key"].(string)
	res.SecretKey, _ = env["secret_key"].(string)
	res.Endpoint, _ = env["endpoint"].(string)
	return res, nil
}

func listS3Buckets(ctx context.Context, cl *client.Client, _ ListS3BucketsInput) (S3BucketListResult, error) {
	items, err := cl.ListS3Buckets(ctx)
	if err != nil {
		return S3BucketListResult{}, err
	}
	return S3BucketListResult{Buckets: items, Count: len(items)}, nil
}

func setS3BucketACL(ctx context.Context, cl *client.Client, in S3BucketACLInput) (OKResult, error) {
	if err := cl.SetS3BucketACL(ctx, in.ID, in.Action); err != nil {
		return OKResult{}, err
	}
	return okResult("bucket ACL set to " + in.Action), nil
}

func deleteS3Bucket(ctx context.Context, cl *client.Client, in DeleteS3BucketInput) (DeleteResult, error) {
	if err := cl.DeleteS3Bucket(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func listS3BucketKeys(ctx context.Context, cl *client.Client, in ListS3BucketKeysInput) (S3BucketKeyListResult, error) {
	items, err := cl.ListS3BucketKeys(ctx, in.BucketID)
	if err != nil {
		return S3BucketKeyListResult{}, err
	}
	return S3BucketKeyListResult{Keys: items, Count: len(items)}, nil
}

func attachS3BucketKey(ctx context.Context, cl *client.Client, in S3BucketKeyInput) (OKResult, error) {
	if in.Permission == "" {
		return OKResult{}, fmt.Errorf("permission is required (read, write, or readwrite)")
	}
	if err := cl.AttachS3BucketKey(ctx, in.BucketID, in.KeyID, in.Permission); err != nil {
		return OKResult{}, err
	}
	return okResult("access key attached"), nil
}

func updateS3BucketKey(ctx context.Context, cl *client.Client, in S3BucketKeyInput) (OKResult, error) {
	if in.Permission == "" {
		return OKResult{}, fmt.Errorf("permission is required (read, write, or readwrite)")
	}
	if err := cl.UpdateS3BucketKey(ctx, in.BucketID, in.KeyID, in.Permission); err != nil {
		return OKResult{}, err
	}
	return okResult("access key permission updated"), nil
}

func detachS3BucketKey(ctx context.Context, cl *client.Client, in S3BucketKeyInput) (OKResult, error) {
	if err := cl.DetachS3BucketKey(ctx, in.BucketID, in.KeyID); err != nil {
		return OKResult{}, err
	}
	return okResult("access key detached"), nil
}

// ── access key handlers ─────────────────────────────────────────────────────

func createS3AccessKey(ctx context.Context, cl *client.Client, in CreateS3AccessKeyInput) (S3AccessKeyResult, error) {
	data, err := cl.CreateS3AccessKey(ctx, map[string]any{"name": in.Name})
	if err != nil {
		return S3AccessKeyResult{}, err
	}
	accessKey, _ := data["access_key"].(string)
	secret, _ := data["secret_key"].(string)
	if accessKey == "" {
		return S3AccessKeyResult{}, fmt.Errorf("create response did not include an access_key")
	}
	// The create response carries no id; resolve it by the public access key.
	obj, err := cl.GetS3AccessKeyByAccessKey(ctx, accessKey)
	if err != nil {
		return S3AccessKeyResult{}, err
	}
	return S3AccessKeyResult{AccessKey: obj, Secret: secret}, nil
}

func getS3AccessKey(ctx context.Context, cl *client.Client, in GetS3AccessKeyInput) (S3AccessKeyResult, error) {
	obj, err := cl.GetS3AccessKey(ctx, in.ID)
	if err != nil {
		return S3AccessKeyResult{}, err
	}
	return S3AccessKeyResult{AccessKey: obj}, nil
}

func listS3AccessKeys(ctx context.Context, cl *client.Client, _ ListS3AccessKeysInput) (S3AccessKeyListResult, error) {
	items, err := cl.ListS3AccessKeys(ctx)
	if err != nil {
		return S3AccessKeyListResult{}, err
	}
	return S3AccessKeyListResult{AccessKeys: items, Count: len(items)}, nil
}

func updateS3AccessKey(ctx context.Context, cl *client.Client, in UpdateS3AccessKeyInput) (OKResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Active != nil {
		fields["active"] = *in.Active
	}
	if err := cl.UpdateS3AccessKey(ctx, in.ID, fields); err != nil {
		return OKResult{}, err
	}
	return okResult("access key updated"), nil
}

func registerS3Tools(s *mcp.Server, deps Deps) {
	// Buckets.
	Register(s, deps, Spec{Name: "user.s3_bucket.create", Description: "Create an S3 bucket (read back by name; create returns no id)."}, createS3Bucket)
	Register(s, deps, Spec{Name: "user.s3_bucket.list", Description: "List all S3 buckets owned by the caller."}, listS3Buckets)
	Register(s, deps, Spec{Name: "user.s3_bucket.get", Description: "Get an S3 bucket by UUID, including its access credentials and endpoint."}, getS3Bucket)
	Register(s, deps, Spec{Name: "user.s3_bucket.set_acl", Description: "Set a bucket's ACL (public, private, upload, or download)."}, setS3BucketACL)
	Register(s, deps, Spec{
		Name:        "user.s3_bucket.delete",
		Description: "Delete an S3 bucket. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteS3Bucket)
	Register(s, deps, Spec{Name: "user.s3_bucket.list_keys", Description: "List access keys attached to a bucket."}, listS3BucketKeys)
	Register(s, deps, Spec{Name: "user.s3_bucket.attach_key", Description: "Attach an access key to a bucket with a permission."}, attachS3BucketKey)
	Register(s, deps, Spec{Name: "user.s3_bucket.update_key", Description: "Change an attached access key's permission on a bucket."}, updateS3BucketKey)
	Register(s, deps, Spec{Name: "user.s3_bucket.detach_key", Description: "Detach an access key from a bucket."}, detachS3BucketKey)

	// Access keys (no delete route in the user API).
	Register(s, deps, Spec{Name: "user.s3_access_key.create", Description: "Create an S3 access key. The secret is returned only once."}, createS3AccessKey)
	Register(s, deps, Spec{Name: "user.s3_access_key.list", Description: "List all S3 access keys (secrets not included)."}, listS3AccessKeys)
	Register(s, deps, Spec{Name: "user.s3_access_key.get", Description: "Get an S3 access key by UUID (secret not included)."}, getS3AccessKey)
	Register(s, deps, Spec{Name: "user.s3_access_key.update", Description: "Update an S3 access key's name or active state."}, updateS3AccessKey)
}
