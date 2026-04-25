// Package cost prices an AIOS run in USD by walking the on-disk audit
// trail and applying a per-model pricing table. The accountant view of
// every run.
//
// Pricing is deliberately hardcoded rather than fetched from a remote — it
// changes rarely, the table is auditable, and the alternative (a network
// dependency to compute a *cost summary*) would be absurd. When a vendor
// changes their rates, edit the table and tag a release.
package cost

// Pricing captures input/output USD rates for one model, expressed per
// million tokens. Keep in sync with vendor pricing pages; cite the
// effective date when bumping these.
type Pricing struct {
	Model           string
	InputUSDPerMTok float64
	OutputUSDPerMTok float64
}

// DefaultPricing is the table consulted when a run does not pin its own.
// Rates are illustrative defaults pulled from public vendor pages as of
// the dated release; users with negotiated rates should override via
// .aios/cost.toml (planned, not yet wired).
//
// Engine-name keys ("claude"/"codex") map to the *default* model AIOS
// invokes via the corresponding CLI. Both CLIs accept a model override —
// when AIOS does not know the actual model used in a round, it falls
// back to the engine-name default.
var DefaultPricing = map[string]Pricing{
	// Anthropic — Claude Opus 4.7 (default for the claude CLI)
	"claude": {Model: "claude-opus-4-7", InputUSDPerMTok: 15.0, OutputUSDPerMTok: 75.0},
	// Anthropic — Claude Sonnet 4.6 (cheaper option)
	"claude-sonnet": {Model: "claude-sonnet-4-6", InputUSDPerMTok: 3.0, OutputUSDPerMTok: 15.0},
	// Anthropic — Claude Haiku 4.5 (cheapest)
	"claude-haiku": {Model: "claude-haiku-4-5", InputUSDPerMTok: 0.80, OutputUSDPerMTok: 4.0},
	// OpenAI — Codex (gpt-5-codex default)
	"codex": {Model: "gpt-5-codex", InputUSDPerMTok: 5.0, OutputUSDPerMTok: 20.0},
}

// PricingFor returns the Pricing entry for the given engine key, falling
// back to a zero-cost entry when the engine is unknown so a missing entry
// degrades the cost summary to "unknown engine, $0.00" rather than
// crashing.
func PricingFor(engine string) Pricing {
	if p, ok := DefaultPricing[engine]; ok {
		return p
	}
	return Pricing{Model: engine + " (unknown)"}
}

// EstimateUSD computes a USD cost from token counts. Tokens are passed
// already split into input vs. output. The function is named "Estimate"
// because vendors round and the table is a snapshot — call results are
// useful for trends, not for reconciling invoices.
func (p Pricing) EstimateUSD(inputTokens, outputTokens int) float64 {
	return (float64(inputTokens)*p.InputUSDPerMTok + float64(outputTokens)*p.OutputUSDPerMTok) / 1_000_000
}
