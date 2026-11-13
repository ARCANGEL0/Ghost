# Free model providers

Ghost loads model catalogs from provider APIs and keeps a local cache for offline starts. The list changes as providers add, remove, or reprice models. `ghost free` shows the catalog Ghost currently has; run `ghost login <provider>` first for providers that need a key.

## Providers

| Provider ID | Access | Setup |
| --- | --- | --- |
| `auto-free` | Tries Kilo and OI VSCode routes, free OpenRouter routes when configured, then local Ollama models | No setup; provider keys and Ollama are optional |
| `openrouter` | Free models marked `:free`, plus the `openrouter/free` router | `ghost login openrouter` |
| `kilo` | Free models marked `:free`; anonymous requests are allowed. `kilo-auto/free` is a stable free route | No key required; `ghost login kilo` is optional |
| `oi-vscode` | Community-hosted endpoint | No key required; availability is not guaranteed |
| `groq` | Free developer tier; account and model rate limits apply | `ghost login groq` |
| `cerebras` | One-time $5 promotional credit; a payment method is required, and credit expires after 30 days | `ghost login cerebras` |
| `googleai` | Models currently listed as free on the Gemini API tier | `ghost login googleai` |
| `cloudflare` | Workers AI free daily allocation; models requiring paid billing are filtered | Set `CLOUDFLARE_ACCOUNT_ID`, then run `ghost login cloudflare` |
| `cohere` | Free trial key with monthly and per-minute limits | `ghost login cohere` |
| `nvidia-nim` | Models available through NVIDIA's developer API access | `ghost login nvidia-nim` |
| `amd-radeon` | Token Factory models whose live catalog marks them free | `ghost login amd-radeon` |
| `aion` | Free daily account quota; limits and model pricing can vary | `ghost login aion` |
| `huggingface` | Inference Providers, covered first by the account's monthly free credits | `ghost login huggingface` |
| `llm7` | Free token quota; only live chat models marked available outside paid usage are listed | `ghost login llm7` |
| `typhoon` | Free research API with per-model rate limits | `ghost login typhoon` |
| `nous-portal` | Models with a live zero-price catalog entry | `ghost login nous-portal` |
| `ollama-local` | Models installed in a local Ollama server | Start Ollama; no key required |

`ghost login <provider>` prompts for a key without echoing it and saves it in Ghost's global configuration. For example:

```sh
ghost login openrouter
ghost login groq
ghost login amd-radeon
ghost login llm7
ghost login typhoon
ghost login nous-portal
ghost free
```

Cloudflare also needs an account ID. Set `CLOUDFLARE_ACCOUNT_ID` in the environment before starting Ghost. The API token needs access to Workers AI for that account.

To use a listed model:

```sh
ghost free use auto-free/auto:free
```

Model IDs can contain `/`, so the command stores the model's full ID with the provider selected from the live catalog.

`AUTO:FREE` tries each eligible route until one responds. It retries stream failures only before the model starts returning text or tool calls, so it does not duplicate partial answers or tool requests. OpenRouter routes are included only when an OpenRouter key is configured; Ollama routes are included only for models installed locally.

## What the free labels mean

- **Free model** means the provider reports a $0 model or a zero-cost developer endpoint. Rate limits, account eligibility, and service terms still apply.
- **Free quota** means inference is free up to the provider's account or daily limit. Some providers stop requests at the limit; others have paid usage after it.
- **Free credits** means Ghost may use a monthly or trial credit balance. It is not a promise of unlimited free requests.
- **Local** means inference runs on your machine. Hardware, power, and storage still have costs.

The list command keeps these categories separate. Check the linked provider page before sending sensitive code. Free plans may have different data-use terms from paid plans.

## Current limits and terms

- [OpenRouter's free model list](https://openrouter.ai/collections/free-models) updates as models change. `:free` variants are rate limited; the current published limits are 20 requests per minute and 50 per day on an unfunded account. Its `openrouter/free` model routes to an eligible free model.
- [Kilo's live model catalog](https://kilo.ai/docs/gateway/models-and-providers) returns model IDs, context, features, and prices. Free models are available without authentication, with a published anonymous limit of 200 requests per hour per IP. The `kilo-auto/free` mapping can change server-side.
- [OpenCode free models](https://opencode.ai/docs/console/harnesses) are restricted to the OpenCode client. OpenCode documents that other harnesses receive HTTP 403, so Ghost does not list these models as usable gateways.
- [Google AI Studio pricing](https://ai.google.dev/gemini-api/docs/pricing) shows which Gemini models have a free API tier. Google notes that free-tier content may be used to improve its products. The models API does not report free-tier eligibility, so Ghost matches its live model list against the current free-model IDs in code.
- [Groq rate limits](https://console.groq.com/docs/rate-limits) and [supported models](https://console.groq.com/docs/models) are account- and model-specific. The live catalog shows what the API exposes; the free developer tier's limits apply to requests.
- [Cerebras pricing](https://www.cerebras.ai/pricing) currently offers a one-time $5 promotional credit for development and evaluation. It requires a valid payment method, expires after 30 days, and does not enroll the account in paid usage.
- [Cloudflare Workers AI pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/) includes 10,000 neurons per day on the free plan. Its [current paid-only model notice](https://developers.cloudflare.com/changelog/post/2026-07-28-models-require-workers-paid/) lists model families Ghost filters from the free catalog.
- [Cohere trial keys](https://docs.cohere.com/docs/rate-limits) allow 1,000 API calls per month, with a 20 requests-per-minute limit for chat models.
- [NVIDIA NIM setup](https://docs.api.nvidia.com/nim/docs/api-quickstart) uses a developer API key and a live `/v1/models` endpoint. The Developer Program provides free access for research, development, and testing; production requires an NVIDIA AI Enterprise license. Review the [NVIDIA NIM product terms](https://docs.api.nvidia.com/nim/docs/product) before use.
- [AMD Token Factory's model endpoint](https://amd-aim.github.io/radeon-cloud-docs/api/models/) returns a `free` field, which Ghost uses to keep only no-cost chat models. AMD describes Token Factory as a free trial and development service, with no production SLA. Its [terms](https://amd-aim.github.io/radeon-cloud-docs/api/terms/) say not to send confidential or sensitive data and disclose short-term prompt and output retention.
- [LLM7](https://llm7.io/) offers free-token access with a daily token cap and request limits. Ghost reads its live catalog and keeps chat models that are marked as available outside paid usage. LLM7 describes the service as educational and research-oriented and does not guarantee model availability.
- [OpenTyphoon](https://docs.opentyphoon.ai/en/faq/) describes its API as a free research showcase in beta. Ghost reads the live model list and keeps the instruction chat models. Its [rate limits](https://docs.opentyphoon.ai/en/rate-limits/) vary by model; OpenTyphoon says production use should go through its partner.
- [Nous Portal](https://portal.nousresearch.com/compare) has a live filter for free models. Ghost keeps chat models whose input and output prices are both explicitly zero. The Portal says API keys use the account's credit balance, so check its free plan and account setup before use.
- [Aion Labs model catalog](https://www.aionlabs.ai/docs/models/) and [rate limits](https://www.aionlabs.ai/docs/rate-limits/) describe its free account quota and model-specific limits.
- [Hugging Face Inference Providers pricing](https://huggingface.co/docs/inference-providers/pricing) gives free accounts a small monthly credit balance. Models have individual prices, and requests can consume that balance.
- [Ollama's API](https://github.com/ollama/ollama/blob/main/docs/api.md) lists models installed in the local server. Ghost does not add model names that are not installed.

## Providers not listed as free gateways

Ghost does not list a provider just because its website or playground is free. For example, [Mistral Free mode does not include API access](https://docs.mistral.ai/admin/billing-usage/usage-limits), and [GitHub Models retired its inference API on July 30, 2026](https://github.blog/changelog/2026-07-01-github-models-is-being-fully-retired-on-july-30-2026/). One-time trial credits and consumer subscriptions are not presented as ongoing free model access.

## Catalog behavior

At startup, Ghost merges built-in provider definitions with its cached catalogs, then refreshes providers that are available to the current account. Providers without a saved key are not queried until their key is available. A failed refresh leaves the last cached catalog in place. `GHOST_DISABLE_PROVIDER_AUTO_UPDATE` disables live refresh; `ghost update-providers` refreshes the general provider registry.

Kilo's `kilo-auto/free` route remains available in the model picker if the live model catalog cannot be reached.

Some APIs return models without a price or free-tier field. For those providers, Ghost uses their published free-tier rules to filter the live catalog. Those rules can lag a provider's pricing changes; report a stale listing with the provider ID and model ID.
