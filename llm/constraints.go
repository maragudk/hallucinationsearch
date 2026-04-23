package llm

import (
	"fmt"
	"math/rand/v2"
)

// resultArchetypes is the 100-item curated list of page formats the result-generation
// prompt rolls over per call. Replaces the old "pick something that hasn't been used"
// dedup, which was defeated by parallel fan-out (later jobs started before the earlier
// ones wrote anything). Sections are preserved as comments to make it easy to tell at
// a glance which bucket a given archetype belongs to; total remains exactly 100.
var resultArchetypes = []string{
	// Reference / encyclopedic (5)
	"Wikipedia-style article",
	"niche-fandom wiki entry",
	"wiki article with a 'multiple issues' banner",
	"wiki stub",
	"dictionary or thesaurus entry",
	// Academic (7)
	"peer-reviewed journal abstract",
	"arXiv-style preprint",
	"retracted-paper notice",
	"conference poster writeup",
	"PhD dissertation excerpt",
	"literature review",
	"etymological dictionary entry",
	// Corporate / bureaucratic (10)
	"corporate press release",
	"quarterly earnings call transcript",
	"corporate About page",
	"terms of service",
	"privacy policy",
	"8-K SEC filing",
	"patent application",
	"white paper",
	"NGO position paper",
	"think-tank report",
	// Forums / community (8)
	"forum thread with replies",
	"Stack Overflow question",
	"accepted Stack Overflow answer",
	"subreddit top post with top comments",
	"Discord message log",
	"mailing list archive",
	"IRC log from 2003",
	"Usenet post",
	// Blogs by era (8)
	"1998 GeoCities personal page",
	"LiveJournal entry",
	"'aesthetic'-era Tumblr post",
	"2012 Medium think-piece",
	"Substack essay",
	"Ghost blog post",
	"abandoned WordPress blog",
	"Neocities-revival personal site",
	// News (7)
	"local newspaper article",
	"AP wire-service report",
	"op-ed column",
	"obituary",
	"classified ad",
	"gossip column item",
	"Sunday magazine profile",
	// Instructional / manual (8)
	"recipe blog with 3000 words of preamble",
	"official owner's manual",
	"safety data sheet (MSDS)",
	"USDA extension bulletin",
	"field manual",
	"first-aid wall poster",
	"DIY tutorial with bad photos",
	"prepper survival guide",
	// Commercial / product (8)
	"DTC product listing",
	"one-star Yelp review",
	"five-star Yelp review",
	"Amazon Q&A section",
	"eBay auction listing",
	"Craigslist missed connection",
	"Etsy listing with an overlong backstory",
	"TripAdvisor hotel review",
	// Legal / official (7)
	"court filing",
	"deposition transcript excerpt",
	"police incident report",
	"FOIA response",
	"health department inspection report",
	"HOA violation notice",
	"municipal council meeting minutes",
	// Niche hobby / local (7)
	"beekeeper association newsletter",
	"model-railroad club page",
	"garden club monthly bulletin",
	"church bulletin announcement",
	"little-league game roundup",
	"PTA meeting minutes",
	"amateur-radio QSL page",
	// Conspiracy / paranormal (7)
	"conspiracy blog post",
	"flat-earth forum thread",
	"cryptid sighting report",
	"UFO incident database entry",
	"haunted location review (TripAdvisor-shaped)",
	"SCP Foundation-style entry",
	"creepypasta",
	// Technical (8)
	"GitHub README",
	"man page",
	"incident post-mortem",
	"API documentation page",
	"changelog or release notes",
	"RFC-style spec (fake)",
	"ISO standard excerpt",
	"CVE advisory",
	// Media / review (5)
	"Ebert-style film review",
	"album liner notes",
	"Goodreads rant review",
	"IMDB trivia section",
	"TVTropes-style article",
	// Misc / weird (5)
	"zine scan (photocopied)",
	"MLM pitch deck",
	"LinkedIn humble-brag post",
	"wedding website",
	"museum exhibit placard",
}

// reliabilitySignals is the 6-item curated list of sourcing styles the result prompt
// rolls over per call to give the fabricated page a credibility posture.
var reliabilitySignals = []string{
	"cites 47 fake academic references",
	"screenshots from a deleted thread as sole evidence",
	"anonymous insider email",
	"\"my uncle told me\"-tier sourcing",
	"zero sources, total confidence",
	"peer-reviewed claim (fake citation included)",
}

// adProductCategories is the 40-item curated list of what's being sold that the ad
// prompt rolls over per call.
var adProductCategories = []string{
	"SaaS tool",
	"consumer app",
	"physical gadget",
	"wellness supplement",
	"online course",
	"newsletter subscription",
	"book",
	"auto insurance",
	"pet insurance",
	"event insurance",
	"life-coach service",
	"crypto project",
	"NFT project",
	"MLM pitch",
	"DTC subscription box",
	"kitchen tool",
	"smart-home device",
	"productivity tool",
	"AI-powered anything",
	"local plumber or electrician or dog walker",
	"real-estate listing",
	"vacation rental",
	"dating service",
	"professional services (lawyer, accountant)",
	"home security system",
	"fringe medicine",
	"quack remedy",
	"gardening service",
	"cleaning service",
	"extended warranty / coverage plan",
	"marketplace platform",
	"hobbyist gear (knitting / fishing / birding)",
	"petcare product",
	"kids' educational toy",
	"streaming service",
	"industry conference",
	"religious retreat",
	"consulting firm",
	"podcast network",
	"weird DIY kit",
}

// adPitchAngles is the 15-item curated list of marketing hooks the ad prompt
// rolls over per call.
var adPitchAngles = []string{
	"fear-based ('you're at risk')",
	"aspirational ('you too can be X')",
	"social-proof-heavy ('10,000 happy customers')",
	"hyper-niche insider ('only the initiated know')",
	"scarcity / discount urgency",
	"FOMO",
	"authority endorsement",
	"curiosity ('one weird trick')",
	"guilt-trip",
	"contrarian ('everyone else is wrong')",
	"nostalgia",
	"green / eco-virtue",
	"origin-story / founder-testimonial",
	"status signaling",
	"problem-awareness ('you didn't know you needed this')",
}

// resultRolls holds the three values rolled for a single result-generation call:
// archetype (page format), weirdness (1-10, 1 = plausibly real, 10 = committed
// absurdity played straight), and reliability signal (sourcing posture).
type resultRolls struct {
	archetype   string
	reliability string
	weirdness   int
}

// constraintsBlock renders the rolled result constraints as the block that gets
// appended to the user prompt. Keep the wording in sync with the "Constraints
// block specifies..." line in resultSystemPrompt.
func (rr resultRolls) constraintsBlock() string {
	return fmt.Sprintf(
		"Constraints:\n"+
			"- Archetype: %s\n"+
			"- Weirdness: %d/10 (1 = plausibly real, 10 = committed-to-the-bit absurd, played straight)\n"+
			"- Reliability signal: %s\n"+
			"\n"+
			"Honor these constraints strictly. The archetype defines the page format. The weirdness number controls how absurd the content may get; keep delivery deadpan regardless.",
		rr.archetype, rr.weirdness, rr.reliability)
}

// adRolls holds the three values rolled for a single ad-generation call:
// product category (what's being sold), pitch angle (the hook), and weirdness
// (1-10, 1 = real-feeling DTC, 10 = committed absurdity pitched straight).
type adRolls struct {
	category  string
	pitch     string
	weirdness int
}

// constraintsBlock renders the rolled ad constraints as the block that gets
// appended to the user prompt. Keep the wording in sync with the "Constraints
// block specifies..." line in adSystemPrompt.
func (ar adRolls) constraintsBlock() string {
	return fmt.Sprintf(
		"Constraints:\n"+
			"- Product category: %s\n"+
			"- Pitch angle: %s\n"+
			"- Weirdness: %d/10 (1 = real-feeling DTC product, 10 = committed-to-the-bit absurd product concept, pitched with a straight face)\n"+
			"\n"+
			"Honor these constraints strictly. The product category defines what's being sold. The pitch angle is the hook. The weirdness number controls how absurd the product may get; the ad copy stays deadpan and marketing-shaped regardless.",
		ar.category, ar.pitch, ar.weirdness)
}

// rollResultConstraints picks a random archetype, weirdness level (1-10), and
// reliability signal. A nil *rand.Rand means use the package-level math/rand/v2
// generator, which is safe for concurrent use; tests pass a seeded rand.NewPCG
// source for determinism.
func rollResultConstraints(r *rand.Rand) resultRolls {
	return resultRolls{
		archetype:   pickString(r, resultArchetypes),
		weirdness:   pickWeirdness(r),
		reliability: pickString(r, reliabilitySignals),
	}
}

// rollAdConstraints picks a random product category, pitch angle, and weirdness
// level (1-10). A nil *rand.Rand means use the package-level math/rand/v2
// generator, which is safe for concurrent use; tests pass a seeded rand.NewPCG
// source for determinism.
func rollAdConstraints(r *rand.Rand) adRolls {
	return adRolls{
		category:  pickString(r, adProductCategories),
		pitch:     pickString(r, adPitchAngles),
		weirdness: pickWeirdness(r),
	}
}

// pickString returns a uniformly-random element from items. r == nil uses the
// global math/rand/v2 generator. Panics on empty slice (callers are package-private
// and the curated lists are never empty).
func pickString(r *rand.Rand, items []string) string {
	if r == nil {
		return items[rand.IntN(len(items))]
	}
	return items[r.IntN(len(items))]
}

// pickWeirdness returns an int in [1, 10].
func pickWeirdness(r *rand.Rand) int {
	if r == nil {
		return rand.IntN(10) + 1
	}
	return r.IntN(10) + 1
}
