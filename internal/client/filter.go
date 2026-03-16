package client

import (
	"encoding/json"
)

// SKU groups for filtering list results
var (
	DevPodSKUs = []string{
		"managed-developer-pods-v2-ca-qc-2",
		"managed-developer-pods-v2",
	}
	KubernetesSKUs = []string{
		"mks-k8s-ca-qc-2",
		"mks-k8s",
		"mks-oneclick",
	}
	VMSKUs = []string{
		"no-gpu-vm",
		"h200-1gpu-vm",
		"h200-2gpu-vm",
		"h200-4gpu-vm",
		"h200-8gpu-vm",
		"a40-1gpu-vm",
		"a40-2gpu-vm",
		"a40-4gpu-vm",
	}
	NotebookSKUs = []string{
		"jupyter-notebook-v4-ca-qc-2",
		"jupyter-notebook-v4",
	}
	InferenceSKUs = []string{
		"inference-vllm-v1",
		"inference-vllm-v1-h100",
	}
	ObjectStorageSKUs = []string{
		"object-storage-vast-ca-qc-2",
		"object-storage-vast",
	}
	SharedFSSKUs = []string{
		"shared-filesystem",
	}
)

// FilterBySKU returns only items whose spec computeProfile or serviceProfile
// name matches one of the given SKUs.
func FilterBySKU(items []json.RawMessage, skus []string) []json.RawMessage {
	allowed := make(map[string]bool, len(skus))
	for _, s := range skus {
		allowed[s] = true
	}

	var out []json.RawMessage
	for _, raw := range items {
		if sku := extractSKU(raw); allowed[sku] {
			out = append(out, raw)
		}
	}
	return out
}

func extractSKU(raw json.RawMessage) string {
	var res struct {
		Spec struct {
			ComputeProfile struct {
				Name string `json:"name"`
			} `json:"computeProfile"`
			ServiceProfile struct {
				Name string `json:"name"`
			} `json:"serviceProfile"`
		} `json:"spec"`
	}
	// spec may be a nested JSON string or object
	var outer struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil || outer.Spec == nil {
		return ""
	}

	// Try direct object first
	if err := json.Unmarshal(outer.Spec, &res.Spec); err == nil {
		if res.Spec.ComputeProfile.Name != "" {
			return res.Spec.ComputeProfile.Name
		}
		if res.Spec.ServiceProfile.Name != "" {
			return res.Spec.ServiceProfile.Name
		}
	}

	// Try spec as JSON string (server may return it encoded)
	var specStr string
	if err := json.Unmarshal(outer.Spec, &specStr); err == nil {
		if err := json.Unmarshal([]byte(specStr), &res.Spec); err == nil {
			if res.Spec.ComputeProfile.Name != "" {
				return res.Spec.ComputeProfile.Name
			}
			if res.Spec.ServiceProfile.Name != "" {
				return res.Spec.ServiceProfile.Name
			}
		}
	}

	return ""
}
