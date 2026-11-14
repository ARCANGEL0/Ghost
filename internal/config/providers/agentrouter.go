package providers

import "charm.land/catwalk/pkg/catwalk"

var AgentRouterProvider = catwalk.Provider{
	ID:          "agentrouter",
	Name:        "AgentRouter (signup credits)",
	APIEndpoint: "https://agentrouter.org/v1",
	APIKey:      "$AGENT_ROUTER_TOKEN",
	Type:        catwalk.TypeOpenAICompat,
}
