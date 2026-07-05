package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Image tools, mirroring the iaas_image resource. Create captures an image from
// an instance (async: poll status to "available", fail "error"). There is no
// SHOW route, so get lists and filters by id (the client handles this). Delete
// is synchronous.

func init() {
	toolRegistrars = append(toolRegistrars, registerImageTools)
}

type CreateImageInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the source instance to capture"`
	Name       string `json:"name" jsonschema:"name for the new image"`
	Cloudinit  string `json:"cloudinit,omitempty" jsonschema:"optional cloud-init to embed"`
	Type       string `json:"type,omitempty" jsonschema:"optional image type"`
}

type GetImageInput struct {
	ID string `json:"id" jsonschema:"UUID of the image"`
}

type ListImagesInput struct{}

type DeleteImageInput struct {
	ID string `json:"id" jsonschema:"UUID of the image to delete"`
	Confirmation
}

type ImageResult struct {
	Image map[string]any `json:"image"`
}

type ImageListResult struct {
	Images []map[string]any `json:"images"`
	Count  int              `json:"count"`
}

func createImage(ctx context.Context, cl *client.Client, in CreateImageInput) (ImageResult, error) {
	fields := map[string]any{"instance_id": in.InstanceID, "name": in.Name}
	if in.Cloudinit != "" {
		fields["cloudinit"] = in.Cloudinit
	}
	if in.Type != "" {
		fields["type"] = in.Type
	}
	created, err := cl.CreateImage(ctx, fields)
	if err != nil {
		return ImageResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return ImageResult{}, fmt.Errorf("create response did not include an image id")
	}
	// Async capture: converge on status "available" (fail "error").
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetImage(ctx, id) },
		"status", []string{"available"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return ImageResult{}, fmt.Errorf("image %s capture did not complete: %w", id, err)
	}
	obj, err := cl.GetImage(ctx, id)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Image: obj}, nil
}

func getImage(ctx context.Context, cl *client.Client, in GetImageInput) (ImageResult, error) {
	obj, err := cl.GetImage(ctx, in.ID)
	if err != nil {
		return ImageResult{}, err
	}
	return ImageResult{Image: obj}, nil
}

func listImages(ctx context.Context, cl *client.Client, _ ListImagesInput) (ImageListResult, error) {
	items, err := cl.ListImages(ctx)
	if err != nil {
		return ImageListResult{}, err
	}
	return ImageListResult{Images: items, Count: len(items)}, nil
}

func deleteImage(ctx context.Context, cl *client.Client, in DeleteImageInput) (DeleteResult, error) {
	if err := cl.DeleteImage(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerImageTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.image.create",
		Description: "Capture an image from an instance and wait until it is available.",
	}, createImage)
	Register(s, deps, Spec{Name: "user.image.list", Description: "List all images owned by the caller."}, listImages)
	Register(s, deps, Spec{Name: "user.image.get", Description: "Get an image by UUID."}, getImage)
	Register(s, deps, Spec{
		Name:        "user.image.delete",
		Description: "Delete an image. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteImage)
}
