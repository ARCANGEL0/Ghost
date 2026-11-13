package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/spf13/cobra"
	"github.com/swadhinbiswas/ghost/internal/config"
	freeProviders "github.com/swadhinbiswas/ghost/internal/config/providers"
)

var freeCmd = &cobra.Command{
	Use:   "free [query]",
	Short: "List and use free models",
	Long: `List all free models Ghost can reach out of the box, or activate one.

Subcommands:
  free [query]      list free models, optionally filtered by name
  free use <id>     activate a model (id format: provider/model)
  free combo <name> create a 3-tier free-only combo
  free install      show gateway setup commands`,
	RunE: runFree,
}

func init() {
	rootCmd.AddCommand(freeCmd)
}

func runFree(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	if len(args) > 0 {
		switch args[0] {
		case "use":
			if len(args) < 2 {
				return fmt.Errorf("usage: ghost free use <provider/model>")
			}
			catalog, err := currentProviders(cmd)
			if err != nil {
				return err
			}
			return useFreeModel(out, args[1], catalog)
		case "combo":
			name := "free-forever"
			if len(args) > 1 {
				name = args[1]
			}
			return createFreeCombo(out, name)
		case "install":
			return printFreeInstall(out)
		}
	}
	catalog, err := currentProviders(cmd)
	if err != nil {
		return err
	}
	filter := ""
	if len(args) > 0 {
		filter = strings.ToLower(args[0])
	}
	return listFreeModels(out, filter, catalog)
}

func currentProviders(cmd *cobra.Command) ([]catwalk.Provider, error) {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")
	store, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, err
	}
	return store.KnownProviders(), nil
}

func listFreeModels(w io.Writer, filter string, catalog []catwalk.Provider) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROVIDER\tMODEL\tNAME\tNOTE")
	fmt.Fprintln(tw, "--------\t-----\t----\t----")
	for _, p := range catalog {
		tier := freeProviders.ProviderTier(string(p.ID))
		if tier == "" {
			continue
		}
		for _, m := range p.Models {
			if !isFreeCatalogModel(string(p.ID), m) {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(m.ID), filter) && !strings.Contains(strings.ToLower(m.Name), filter) {
				continue
			}
			note := freeTierNote(tier, m)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.ID, m.ID, m.Name, note)
		}
	}
	return tw.Flush()
}

func useFreeModel(w io.Writer, id string, catalog []catwalk.Provider) error {
	// id is "provider/model"
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected format: provider/model (got %q)", id)
	}
	prov, model := parts[0], parts[1]
	for _, p := range catalog {
		for _, m := range p.Models {
			if isFreeCatalogModel(string(p.ID), m) && (m.ID == id || (string(p.ID) == prov && m.ID == model)) {
				providerID := string(p.ID)
				modelID := m.ID
				fmt.Fprintf(w, "✓ set model to %s/%s\n", providerID, modelID)
				fmt.Fprintln(w, "(update ~/.ghost/config.json with:")
				fmt.Fprintf(w, "  \"models\": {\"large\": {\"provider\":%q,\"model\":%q}, \"small\": {\"provider\":%q,\"model\":%q}}\n", providerID, modelID, providerID, modelID)
				fmt.Fprintln(w, ")")
				return nil
			}
		}
	}
	return fmt.Errorf("model %q not found in any free provider", id)
}

func createFreeCombo(w io.Writer, name string) error {
	fmt.Fprintf(w, "Free fallback suggestion %q:\n", name)
	fmt.Fprintln(w, "Use these provider/model IDs in your routing setup:")
	fmt.Fprintln(w, "auto-free/auto:free -> kilo/kilo-auto/free -> openrouter/openrouter/free")
	return nil
}

func printFreeInstall(w io.Writer) error {
	fmt.Fprintln(w, "Browse current free models with: ghost free")
	fmt.Fprintln(w, "Use one with: ghost free use <provider/model>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Kilo's free models need no API key. Start Ollama for local models.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "For a gateway API key, run: ghost login <provider>")
	fmt.Fprintln(w, "Keyed gateways include OpenRouter, Groq, Cerebras, Google AI, Cloudflare, Cohere, AMD Radeon, Aion, Hugging Face, LLM7, OpenTyphoon, Nous Portal, and NVIDIA NIM.")
	fmt.Fprintln(w, "Cloudflare also needs CLOUDFLARE_ACCOUNT_ID in the environment.")
	fmt.Fprintln(w, "See docs/free-model-providers.md for setup and current limits.")
	return nil
}

func isFreeCatalogModel(provider string, model catwalk.Model) bool {
	switch provider {
	case "openrouter":
		return strings.HasSuffix(model.ID, ":free") || model.ID == "openrouter/free"
	case "kilo":
		return strings.HasSuffix(model.ID, ":free") || model.ID == "kilo-auto/free"
	case "auto-free":
		return model.ID == "auto:free"
	case "cloudflare":
		id := strings.ToLower(model.ID)
		return !strings.Contains(id, "kimi-k2") && !strings.Contains(id, "glm-5.") && !strings.Contains(id, "deepseek-v4-")
	default:
		return true
	}
}

func freeTierNote(tier freeProviders.Tier, model catwalk.Model) string {
	switch tier {
	case freeProviders.TierFree:
		return "FREE MODEL"
	case freeProviders.TierLocal:
		return "LOCAL"
	case freeProviders.TierQuota:
		if model.CostPer1MIn > 0 || model.CostPer1MOut > 0 {
			return fmt.Sprintf("FREE QUOTA (listed price $%.2f/$%.2f per 1M)", model.CostPer1MIn, model.CostPer1MOut)
		}
		return "FREE QUOTA"
	case freeProviders.TierCredits:
		if model.CostPer1MIn > 0 || model.CostPer1MOut > 0 {
			return fmt.Sprintf("CREDITS ($%.2f/$%.2f per 1M)", model.CostPer1MIn, model.CostPer1MOut)
		}
		return "FREE CREDITS"
	default:
		return ""
	}
}
