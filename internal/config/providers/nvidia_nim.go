package providers

import "charm.land/catwalk/pkg/catwalk"

var NvidiaNimProvider = catwalk.Provider{
	ID:          "nvidia-nim",
	Name:        "NVIDIA NIM (free developer credits)",
	APIEndpoint: "https://integrate.api.nvidia.com/v1",
	APIKey:      "$NVIDIA_API_KEY",
	Type:        catwalk.TypeOpenAICompat,
}
