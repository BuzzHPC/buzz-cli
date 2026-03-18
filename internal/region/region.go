package region

import "fmt"

// Region represents a BuzzHPC deployment region.
type Region string

const (
	CAQC1 Region = "ca-qc-1"
	CAQC2 Region = "ca-qc-2"
)

// All returns every supported region.
func All() []Region {
	return []Region{CAQC1, CAQC2}
}

// String returns the region as a string.
func (r Region) String() string {
	return string(r)
}

// Parse validates and returns a Region from a string.
// Returns an error if the region is not recognised.
func Parse(s string) (Region, error) {
	for _, r := range All() {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fmt.Errorf(
		"unknown region %q — valid regions: ca-qc-1, ca-qc-2",
		s,
	)
}

// skuTable maps (resourceType, region) → SKU string.
// Resources that have no regional variant use the same SKU for all regions.
var skuTable = map[string]map[Region]string{
	"devpod": {
		CAQC1: "managed-developer-pods-v2",
		CAQC2: "managed-developer-pods-v2-ca-qc-2",
	},
	"notebook": {
		CAQC1: "jupyter-notebook-v4",
		CAQC2: "jupyter-notebook-v4-ca-qc-2",
	},
	"kubernetes": {
		CAQC1: "mks-k8s",
		CAQC2: "mks-k8s-ca-qc-2",
	},
	"inference": {
		CAQC1: "inference-vllm-v1",
		CAQC2: "inference-vllm-v1",
	},
	"object-storage": {
		CAQC1: "object-storage-vast",
		CAQC2: "object-storage-vast-ca-qc-2",
	},
	"shared-filesystem": {
		CAQC1: "shared-filesystem",
		CAQC2: "shared-filesystem",
	},
	"vm": {
		CAQC1: "no-gpu-vm",
		CAQC2: "no-gpu-vm",
	},
}

// SKU returns the correct SKU string for the given resource type and region.
// Returns an error if the combination is not found.
func SKU(resourceType string, r Region) (string, error) {
	byRegion, ok := skuTable[resourceType]
	if !ok {
		return "", fmt.Errorf("unknown resource type %q", resourceType)
	}
	sku, ok := byRegion[r]
	if !ok {
		return "", fmt.Errorf("no SKU for resource type %q in region %q", resourceType, r)
	}
	return sku, nil
}
