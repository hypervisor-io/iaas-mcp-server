package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM image tools (contract C5/C6). An image is built once from a
// base/dockerfile/oci/git source and stamped into immutable versions;
// microVMs run a version. delete is confirm-gated (destructive: it removes
// every version; the API refuses with 409 while a microVM still references
// the image).
//
// C5: list/get surface the image's `os` block ({id, family, name, version,
// codename, arch, pkg}) and `features` ({sshd, envd, vcagent}) whenever the
// platform sets them - this file passes the API's image object through
// unmodified (map[string]any), so no separate wiring is needed here.
// C6: every custom image is built on top of a catalog base, so
// base_image_id is now REQUIRED for every source_kind.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmImageTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmImagesInput struct {
	Search string `json:"search,omitempty" jsonschema:"filter by name substring"`
	Kind   string `json:"kind,omitempty" jsonschema:"filter by source_kind: base, dockerfile, oci, git"`
	Status string `json:"status,omitempty" jsonschema:"filter by build status: pending, building, ready, error"`
}

type GetMicrovmImageInput struct {
	ID string `json:"id" jsonschema:"UUID of the image"`
}

// CreateMicrovmImageInput carries the Create Image body flattened: the
// nested "source" and "auth" objects are built from the source_kind-specific
// and auth_kind-specific fields, so an agent never assembles nested JSON.
type CreateMicrovmImageInput struct {
	Name             string         `json:"name" jsonschema:"image name, max 100 characters"`
	SourceKind       string         `json:"source_kind" jsonschema:"dockerfile, oci or git"`
	LocationID       string         `json:"location_id" jsonschema:"UUID of the location whose builder node runs the build"`
	BaseImageID      string         `json:"base_image_id" jsonschema:"required for every source_kind (C6): the catalog base the build runs on top of - an enabled, ready platform base image, or one of the account's own images"`
	Description      string         `json:"description,omitempty" jsonschema:"free-text description, max 2000 characters"`
	Dockerfile       string         `json:"dockerfile,omitempty" jsonschema:"Dockerfile contents, required when source_kind is dockerfile"`
	SourceImage      string         `json:"source_image,omitempty" jsonschema:"container image reference, required when source_kind is oci"`
	SourceRepo       string         `json:"source_repo,omitempty" jsonschema:"git repository URL, required when source_kind is git"`
	SourceBranch     string         `json:"source_branch,omitempty" jsonschema:"git branch, optional when source_kind is git"`
	AuthKind         string         `json:"auth_kind,omitempty" jsonschema:"registry or git_source, when the source needs credentials"`
	RegistryUsername string         `json:"registry_username,omitempty" jsonschema:"registry username, required when auth_kind is registry"`
	RegistryPassword string         `json:"registry_password,omitempty" jsonschema:"registry password, required when auth_kind is registry; stored encrypted and never returned"`
	GitSourceID      string         `json:"git_source_id,omitempty" jsonschema:"UUID of an owned git source, required when auth_kind is git_source"`
	Env              string         `json:"env,omitempty" jsonschema:"newline-separated KEY=value environment baked into the image, stored encrypted and never returned"`
	LifecycleHooks   map[string]any `json:"lifecycle_hooks,omitempty" jsonschema:"default lifecycle hooks for microVMs running this image (run/resume/suspend/terminate, each {enabled, timeout, payload?})"`
	BuildHooks       map[string]any `json:"build_hooks,omitempty" jsonschema:"build hooks {ready: {enabled, timeout}, validate: {enabled, timeout}}"`
}

type BuildMicrovmImageInput struct {
	ID         string `json:"id" jsonschema:"UUID of the image"`
	LocationID string `json:"location_id" jsonschema:"UUID of the location whose builder node runs the new version"`
}

// DeleteMicrovmImageInput deletes an image and every version. Confirm-gated
// (destructive) - microVMs already created from it keep their current run
// but can never deploy again.
type DeleteMicrovmImageInput struct {
	ID string `json:"id" jsonschema:"UUID of the image to delete"`
	Confirmation
}

type MicrovmImageResult struct {
	Image map[string]any `json:"image"`
}

// MicrovmImageShowResult is the SHOW envelope: the image plus its versions
// and the count of microVMs running it.
type MicrovmImageShowResult struct {
	Image         map[string]any   `json:"image"`
	Versions      []map[string]any `json:"versions"`
	MicrovmsCount int              `json:"microvms_count"`
}

type MicrovmImageListResult struct {
	Images []map[string]any `json:"images"`
	Count  int              `json:"count"`
}

type MicrovmImageVersionResult struct {
	Version map[string]any `json:"version"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovmImages(ctx context.Context, cl *client.Client, in ListMicrovmImagesInput) (MicrovmImageListResult, error) {
	items, err := cl.ListMicrovmImages(ctx, in.Search, in.Kind, in.Status)
	if err != nil {
		return MicrovmImageListResult{}, err
	}
	return MicrovmImageListResult{Images: items, Count: len(items)}, nil
}

func getMicrovmImage(ctx context.Context, cl *client.Client, in GetMicrovmImageInput) (MicrovmImageShowResult, error) {
	env, err := cl.GetMicrovmImage(ctx, in.ID)
	if err != nil {
		return MicrovmImageShowResult{}, err
	}
	image, _ := env["image"].(map[string]any)
	count, _ := env["microvms_count"].(float64)
	return MicrovmImageShowResult{
		Image:         image,
		Versions:      asObjectList(env["versions"]),
		MicrovmsCount: int(count),
	}, nil
}

// createMicrovmImage maps the flat source/auth inputs onto the nested
// "source" and "auth" objects the API expects, sending only the fields the
// caller actually set so the API applies its own defaults. base_image_id and
// location_id are always sent - both are required by the API (C6).
func createMicrovmImage(ctx context.Context, cl *client.Client, in CreateMicrovmImageInput) (MicrovmImageResult, error) {
	source := map[string]any{}
	switch in.SourceKind {
	case "dockerfile":
		source["dockerfile"] = in.Dockerfile
	case "oci":
		source["image"] = in.SourceImage
	case "git":
		source["repo"] = in.SourceRepo
		if in.SourceBranch != "" {
			source["branch"] = in.SourceBranch
		}
	}

	body := map[string]any{
		"name":          in.Name,
		"source_kind":   in.SourceKind,
		"source":        source,
		"location_id":   in.LocationID,
		"base_image_id": in.BaseImageID,
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.AuthKind != "" {
		auth := map[string]any{"kind": in.AuthKind}
		if in.AuthKind == "registry" {
			auth["username"] = in.RegistryUsername
			auth["password"] = in.RegistryPassword
		}
		if in.AuthKind == "git_source" {
			auth["git_source_id"] = in.GitSourceID
		}
		body["auth"] = auth
	}
	if in.Env != "" {
		body["env"] = in.Env
	}
	if in.LifecycleHooks != nil {
		body["lifecycle_hooks"] = in.LifecycleHooks
	}
	if in.BuildHooks != nil {
		body["build_hooks"] = in.BuildHooks
	}

	image, err := cl.CreateMicrovmImage(ctx, body)
	if err != nil {
		return MicrovmImageResult{}, err
	}
	return MicrovmImageResult{Image: image}, nil
}

func buildMicrovmImage(ctx context.Context, cl *client.Client, in BuildMicrovmImageInput) (MicrovmImageVersionResult, error) {
	version, err := cl.BuildMicrovmImage(ctx, in.ID, in.LocationID)
	if err != nil {
		return MicrovmImageVersionResult{}, err
	}
	return MicrovmImageVersionResult{Version: version}, nil
}

func deleteMicrovmImage(ctx context.Context, cl *client.Client, in DeleteMicrovmImageInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmImage(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerMicrovmImageTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.image.list", Description: "List the microVM images visible to the caller (platform bases plus own builds), filterable by name, source kind and build status. Each image carries its os {family, name, version, codename, arch} and features {sshd, envd, vcagent} when set."}, listMicrovmImages)
	Register(s, deps, Spec{Name: "user.microvm.image.get", Description: "Get an image by UUID, including its versions, how many microVMs run it, and its os/features metadata."}, getMicrovmImage)
	Register(s, deps, Spec{Name: "user.microvm.image.create", Description: "Create a custom image built on top of a catalog base (C6) from a Dockerfile, container image or git repository, and dispatch its first build."}, createMicrovmImage)
	Register(s, deps, Spec{Name: "user.microvm.image.build", Description: "Build a new version of an image from the same source, on a builder node in location_id."}, buildMicrovmImage)
	Register(s, deps, Spec{Name: "user.microvm.image.delete", Description: "Delete an image and every version. Refused with 409 while a microVM still references it. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovmImage)
}
