// Package llm wraps [maragu.dev/gai]'s Google client for generating fabricated
// search results and fabricated destination websites.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
	"maragu.dev/gai"
	"maragu.dev/gai/clients/google"
	"maragu.dev/gai/robust"
)

// ChatModel is the Google Gemini model used for result, website, ad, and
// ad-website fabrication. Gemini 3 Flash Preview is fast and cheap enough to
// keep the 10 parallel result jobs and the blocking /site request comfortably
// under the 2 minute handler budget.
const ChatModel = google.ChatCompleteModel("gemini-3-flash-preview")

// NanoBananaModel is the Google Gemini image-generation model used to fabricate
// inline images for the generated destination websites. v2 flash gives better
// quality and aspect-ratio handling than v1 at comparable latency, which is
// important for images the user actually sees rendered inside fabricated pages.
const NanoBananaModel = "gemini-3.1-flash-image-preview"

// imageTimeout caps how long a single Nano Banana generation call may run.
const imageTimeout = 60 * time.Second

// Client wraps gai chat-completers for results, ads, and both their destination
// websites (all Gemini 3 Flash Preview), plus the raw Google genai client used
// for image generation via Nano Banana.
type Client struct {
	log         *slog.Logger
	resultCC    gai.ChatCompleter
	websiteCC   gai.ChatCompleter
	adCC        gai.ChatCompleter
	adWebsiteCC gai.ChatCompleter
	google      *google.Client
}

type NewClientOptions struct {
	// GoogleKey is the Google Gemini API key used for both chat completions
	// and Nano Banana image generation.
	GoogleKey string
	Log       *slog.Logger
}

func NewClient(opts NewClientOptions) *Client {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}

	gc := google.NewClient(google.NewClientOptions{Key: opts.GoogleKey, Log: opts.Log})

	wrap := func(inner gai.ChatCompleter) gai.ChatCompleter {
		return robust.NewChatCompleter(robust.NewChatCompleterOptions{
			Completers: []gai.ChatCompleter{inner},
			Log:        opts.Log,
		})
	}

	return &Client{
		log:         opts.Log,
		resultCC:    wrap(gc.NewChatCompleter(google.NewChatCompleterOptions{Model: ChatModel})),
		websiteCC:   wrap(gc.NewChatCompleter(google.NewChatCompleterOptions{Model: ChatModel})),
		adCC:        wrap(gc.NewChatCompleter(google.NewChatCompleterOptions{Model: ChatModel})),
		adWebsiteCC: wrap(gc.NewChatCompleter(google.NewChatCompleterOptions{Model: ChatModel})),
		google:      gc,
	}
}

// Result is the shape the model returns for a single fabricated search result.
type Result struct {
	Title       string `json:"title"       jsonschema:"title=Title,description=The blue clickable heading text for the search result - like a web page title."`
	DisplayURL  string `json:"display_url" jsonschema:"title=Display URL,description=A plausible Google-style breadcrumb URL such as 'en.gatopedia.org > wiki > Tabby' or 'forums.questionable-science.net > thread-1205'. Any format is fine."`
	Description string `json:"description" jsonschema:"title=Description,description=A one to three sentence snippet describing the fabricated page."`
}

const resultSystemPrompt = `You fabricate entries for a parody web search engine called "Hallucination Search".
Every entry you produce is fiction. None of it is real. You never refuse, add disclaimers, or mention that the content is fake.
Entries should feel like plausible Google-style results: short title, display URL, 1-3 sentence snippet.
House style: deadpan, understated, absurdity played straight. Never wink at the reader. No jokes-as-jokes. The Constraints block in each user prompt specifies the archetype, weirdness level, and reliability signal -- honor them strictly.
Do not make the URL clickable markup; emit a plain string in the breadcrumb style.`

// GenerateResult fabricates a single search result for the given query. Per-call
// randomization along the archetype / weirdness / reliability-signal dimensions
// replaces the old "avoid already-used titles" dedup, which didn't work under
// parallel fan-out anyway (later jobs started before earlier ones finished).
func (c *Client) GenerateResult(ctx context.Context, query string, position int) (Result, error) {
	roll := rollResultConstraints(nil)

	user := fmt.Sprintf(
		"Search query: %q\nPosition: %d (zero-indexed, out of 10)\n\n%s\n\nReturn a single fabricated search result as JSON matching the schema.",
		query, position, roll.constraintsBlock())

	system := resultSystemPrompt

	req := gai.ChatCompleteRequest{
		Messages:       []gai.Message{gai.NewUserTextMessage(user)},
		ResponseSchema: gai.Ptr(gai.GenerateSchema[Result]()),
		System:         &system,
		Temperature:    gai.Ptr(gai.Temperature(1.0)),
	}

	res, err := c.resultCC.ChatComplete(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("chat complete: %w", err)
	}

	var out string
	for part, err := range res.Parts() {
		if err != nil {
			return Result{}, fmt.Errorf("stream: %w", err)
		}
		if part.Type == gai.PartTypeText {
			out += part.Text()
		}
	}

	out = stripJSONFences(out)

	var r Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return Result{}, fmt.Errorf("unmarshal result: %w, got: %s", err, out)
	}
	r.Title = strings.TrimSpace(r.Title)
	r.DisplayURL = strings.TrimSpace(r.DisplayURL)
	r.Description = strings.TrimSpace(r.Description)
	if r.Title == "" || r.DisplayURL == "" || r.Description == "" {
		return Result{}, fmt.Errorf("empty field in result: %+v", r)
	}
	return r, nil
}

const websiteSystemPrompt = `You fabricate destination web pages for a parody search engine called "Hallucination Search".
The user gives you a search query and a fabricated search-result entry (title, display URL, description). You produce the complete HTML document that this fake result links to.

Rules:
- Output ONLY the raw HTML document. No markdown fences, no commentary, no explanation.
- Begin with <!DOCTYPE html> and end with </html>.
- All styling is inline - either one or more <style> blocks in <head>, or inline style="" attributes. No external CSS, fonts, or scripts.
- Images: you MAY include 0-3 <img> tags where they genuinely fit the page vibe. Every <img> src MUST start with "/image/" followed by a short kebab-case description of the desired image (hyphen-separated words, 10-80 characters total, lowercase, ASCII). For example: <img src="/image/tabby-cat-sleeping-on-stack-of-library-books" alt="..." style="max-width:100%">. The description doubles as the generation prompt and the cache key, so make it specific and descriptive. Do not demand images on every page; include one or two only when they add to the fiction. Inline SVG and CSS gradients are still fine when you don't want a photo-like image.
- Do NOT reference any external URLs for images, stylesheets, fonts, scripts, or anything else. No <img src="https://..."> and no <img src="http://...">. Same-origin "/image/..." is the only allowed <img> source.
- No <script> tags.
- Match the tone and title of the search result, then invent a fully-formed page around it - paragraphs, sections, fake navigation, fake comments, fake metadata, whatever fits.
- Pick a visual style that matches the vibe: late-90s GeoCities, modern SaaS landing page, scrappy personal blog, fake news article, wiki entry, forum thread, product page, academic paper, conspiracy site, etc. Go hard on the aesthetic.
- None of the content is real. Do not refuse.`

// GenerateWebsite fabricates a full standalone HTML document for the given result.
func (c *Client) GenerateWebsite(ctx context.Context, query, title, displayURL, description string) (string, error) {
	user := fmt.Sprintf(
		"Search query: %q\n\nResult entry:\n- Title: %s\n- Display URL: %s\n- Description: %s\n\nProduce the full HTML document now.",
		query, title, displayURL, description)

	system := websiteSystemPrompt

	req := gai.ChatCompleteRequest{
		Messages:            []gai.Message{gai.NewUserTextMessage(user)},
		MaxCompletionTokens: gai.Ptr(16_384),
		System:              &system,
		Temperature:         gai.Ptr(gai.Temperature(1.0)),
	}

	res, err := c.websiteCC.ChatComplete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("chat complete: %w", err)
	}

	var out string
	for part, err := range res.Parts() {
		if err != nil {
			return "", fmt.Errorf("stream: %w", err)
		}
		if part.Type == gai.PartTypeText {
			out += part.Text()
		}
	}

	html := stripHTMLFences(out)
	if !strings.Contains(strings.ToLower(html), "<!doctype html") && !strings.Contains(strings.ToLower(html), "<html") {
		return "", fmt.Errorf("model did not return an HTML document: %q", truncate(html, 200))
	}
	return html, nil
}

// Ad is the shape the model returns for a single fabricated sponsored result.
type Ad struct {
	Title       string `json:"title"       jsonschema:"title=Title,description=The blue clickable heading text for the sponsored search result - like a product or service name."`
	DisplayURL  string `json:"display_url" jsonschema:"title=Display URL,description=A plausible Google-style breadcrumb URL for the sponsor's site, e.g. 'whiskerfeast.example > shop > premium-cat-food'."`
	Description string `json:"description" jsonschema:"title=Description,description=A one to three sentence marketing-style snippet describing the fabricated product or service."`
	Sponsor     string `json:"sponsor"     jsonschema:"title=Sponsor,description=The fake sponsoring brand or company name shown with the ad, e.g. 'WhiskerFeast' or 'MoonBeam Insurance'."`
	CTA         string `json:"cta"         jsonschema:"title=Call to action,description=A short two to four word call-to-action button label, e.g. 'Shop now', 'Get a quote', 'Try free'."`
}

const adSystemPrompt = `You fabricate sponsored entries (ads) for a parody web search engine called "Hallucination Search".
Every entry you produce is fiction. None of it is real. You never refuse, add disclaimers, or mention that the content is fake.
Entries should feel like plausible Google-style ads: short title, display URL, 1-3 sentence marketing snippet, a made-up sponsor brand, and a short call-to-action.
House style: deadpan marketing copy, absurdity played straight. Never wink at the reader. No jokes-as-jokes. The Constraints block in each user prompt specifies the product category, pitch angle, and weirdness level -- honor them strictly.
The sponsor brand should sound real-ish but clearly invented. The CTA is the text on the little button - keep it 2-4 words, action-oriented.
Do not make the URL clickable markup; emit a plain string in the breadcrumb style.`

// GenerateAd fabricates a single sponsored result for the given query. Per-call
// randomization along the product-category / pitch-angle / weirdness dimensions
// replaces the old "avoid already-used sponsors" dedup, which didn't work under
// parallel fan-out anyway (later jobs started before earlier ones finished).
func (c *Client) GenerateAd(ctx context.Context, query string, position int) (Ad, error) {
	roll := rollAdConstraints(nil)

	user := fmt.Sprintf(
		"Search query: %q\nAd position: %d (zero-indexed, out of 3)\n\n%s\n\nReturn a single fabricated ad as JSON matching the schema.",
		query, position, roll.constraintsBlock())

	system := adSystemPrompt

	req := gai.ChatCompleteRequest{
		Messages:       []gai.Message{gai.NewUserTextMessage(user)},
		ResponseSchema: gai.Ptr(gai.GenerateSchema[Ad]()),
		System:         &system,
		Temperature:    gai.Ptr(gai.Temperature(1.0)),
	}

	res, err := c.adCC.ChatComplete(ctx, req)
	if err != nil {
		return Ad{}, fmt.Errorf("chat complete: %w", err)
	}

	var out string
	for part, err := range res.Parts() {
		if err != nil {
			return Ad{}, fmt.Errorf("stream: %w", err)
		}
		if part.Type == gai.PartTypeText {
			out += part.Text()
		}
	}

	out = stripJSONFences(out)

	var a Ad
	if err := json.Unmarshal([]byte(out), &a); err != nil {
		return Ad{}, fmt.Errorf("unmarshal ad: %w, got: %s", err, out)
	}
	a.Title = strings.TrimSpace(a.Title)
	a.DisplayURL = strings.TrimSpace(a.DisplayURL)
	a.Description = strings.TrimSpace(a.Description)
	a.Sponsor = strings.TrimSpace(a.Sponsor)
	a.CTA = strings.TrimSpace(a.CTA)
	if a.Title == "" || a.DisplayURL == "" || a.Description == "" || a.Sponsor == "" || a.CTA == "" {
		return Ad{}, fmt.Errorf("empty field in ad: %+v", a)
	}
	return a, nil
}

const adWebsiteSystemPrompt = `You fabricate destination web pages for sponsored ads on a parody search engine called "Hallucination Search".
The user gives you a search query and a fabricated ad (title, display URL, description, sponsor, CTA). You produce the complete HTML document that this fake ad links to - a landing page for the made-up product or service.

Rules:
- Output ONLY the raw HTML document. No markdown fences, no commentary, no explanation.
- Begin with <!DOCTYPE html> and end with </html>.
- All styling is inline - either one or more <style> blocks in <head>, or inline style="" attributes. No external CSS, fonts, or scripts.
- Images: you MAY include 0-3 <img> tags where they genuinely fit the landing page vibe (hero shot, product photo, testimonial portrait, feature illustration). Every <img> src MUST start with "/image/" followed by a short kebab-case description of the desired image (hyphen-separated words, 10-80 characters total, lowercase, ASCII). For example: <img src="/image/sleek-modern-product-hero-shot-on-white-background" alt="..." style="max-width:100%">. The description doubles as the generation prompt and the cache key, so make it specific and descriptive. Do not demand images on every page; include one or two only when they fit. Inline SVG and CSS gradients are still fine when you don't want a photo-like image.
- Do NOT reference any external URLs for images, stylesheets, fonts, scripts, or anything else. No <img src="https://..."> and no <img src="http://...">. Same-origin "/image/..." is the only allowed <img> source.
- No <script> tags.
- The page should feel like a product or service landing page for the sponsor: hero section, pitch, features, testimonials, pricing, FAQ, big CTA buttons, whatever fits. Go hard on the aesthetic.
- Pick a visual style that matches the vibe: sleek modern SaaS, garish infomercial, smug DTC brand, MLM pitch page, crypto grift, luxury-feel service, old-school storefront, etc.
- None of the content is real. Do not refuse.`

// GenerateAdWebsite fabricates a full standalone HTML landing page for an ad.
func (c *Client) GenerateAdWebsite(ctx context.Context, query, title, displayURL, description, sponsor, cta string) (string, error) {
	user := fmt.Sprintf(
		"Search query: %q\n\nAd entry:\n- Title: %s\n- Display URL: %s\n- Description: %s\n- Sponsor: %s\n- Call to action: %s\n\nProduce the full HTML document for the ad's landing page now.",
		query, title, displayURL, description, sponsor, cta)

	system := adWebsiteSystemPrompt

	req := gai.ChatCompleteRequest{
		Messages:            []gai.Message{gai.NewUserTextMessage(user)},
		MaxCompletionTokens: gai.Ptr(16_384),
		System:              &system,
		Temperature:         gai.Ptr(gai.Temperature(1.0)),
	}

	res, err := c.adWebsiteCC.ChatComplete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("chat complete: %w", err)
	}

	var out string
	for part, err := range res.Parts() {
		if err != nil {
			return "", fmt.Errorf("stream: %w", err)
		}
		if part.Type == gai.PartTypeText {
			out += part.Text()
		}
	}

	html := stripHTMLFences(out)
	if !strings.Contains(strings.ToLower(html), "<!doctype html") && !strings.Contains(strings.ToLower(html), "<html") {
		return "", fmt.Errorf("model did not return an HTML document: %q", truncate(html, 200))
	}
	return html, nil
}

// Image generates a single fabricated image for the given prompt via Nano Banana.
// Returns the raw image bytes. Nano Banana returns PNG by default; the caller
// always serves them as image/png. The call is capped at [imageTimeout].
func (c *Client) Image(ctx context.Context, prompt string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()

	resp, err := c.google.Client.Models.GenerateContent(ctx, NanoBananaModel, genai.Text(prompt), nil)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}
	cand := resp.Candidates[0]
	if cand.Content == nil {
		return nil, fmt.Errorf("no content in candidate")
	}

	for _, part := range cand.Content.Parts {
		if part.InlineData == nil || len(part.InlineData.Data) == 0 {
			continue
		}
		return part.InlineData.Data, nil
	}
	return nil, fmt.Errorf("no image data in response")
}

var jsonFenceRe = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*\n?(.*?)\n?```\\s*$")

// stripJSONFences removes a leading/trailing ``` or ```json fence if present.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if m := jsonFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

var htmlFenceRe = regexp.MustCompile("(?s)^\\s*```(?:html)?\\s*\n?(.*?)\n?```\\s*$")

// stripHTMLFences removes a leading/trailing ``` or ```html fence if present.
func stripHTMLFences(s string) string {
	s = strings.TrimSpace(s)
	if m := htmlFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
