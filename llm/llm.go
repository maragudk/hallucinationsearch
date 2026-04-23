// Package llm wraps [maragu.dev/gai]'s Anthropic client for generating fabricated
// search results and fabricated destination websites.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"maragu.dev/gai"
	"maragu.dev/gai/clients/anthropic"
)

// HaikuModel is the Anthropic model used for both result fabrication and website fabrication.
// Haiku is cheap and fast enough to keep the 10 parallel result jobs and the blocking
// /site request comfortably under the 2 minute handler budget.
const HaikuModel = anthropic.ChatCompleteModelClaudeHaiku4_5Latest

// Client wraps gai chat-completers for both results and websites (both on Haiku).
type Client struct {
	log       *slog.Logger
	resultCC  gai.ChatCompleter
	websiteCC gai.ChatCompleter
}

type NewClientOptions struct {
	Key string
	Log *slog.Logger
}

func NewClient(opts NewClientOptions) *Client {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}

	c := anthropic.NewClient(anthropic.NewClientOptions{Key: opts.Key, Log: opts.Log})

	return &Client{
		log:       opts.Log,
		resultCC:  c.NewChatCompleter(anthropic.NewChatCompleterOptions{Model: HaikuModel}),
		websiteCC: c.NewChatCompleter(anthropic.NewChatCompleterOptions{Model: HaikuModel}),
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
Tone should vary wildly across positions for the same query - a straight-faced fake wiki article, a paranoid forum rant, an overt parody, a product listing, a local news page, an academic paper, a recipe blog, a conspiracy site, a press release, a personal homepage, etc. Pick something that hasn't been used already for this query, unless the query is very narrow.
Do not make the URL clickable markup; emit a plain string in the breadcrumb style.`

// GenerateResult fabricates a single search result for the given query, avoiding titles already used.
func (c *Client) GenerateResult(ctx context.Context, query string, position int, existingTitles []string) (Result, error) {
	var avoid string
	if len(existingTitles) > 0 {
		var b strings.Builder
		b.WriteString("\n\nAlready-generated titles for this query (pick a distinct angle):\n")
		for _, t := range existingTitles {
			b.WriteString("- ")
			b.WriteString(t)
			b.WriteString("\n")
		}
		avoid = b.String()
	}

	user := fmt.Sprintf(
		"Search query: %q\nPosition: %d (zero-indexed, out of 10)%s\n\nReturn a single fabricated search result as JSON matching the schema.",
		query, position, avoid)

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
- All styling is inline - either one or more <style> blocks in <head>, or inline style="" attributes. No external CSS, fonts, scripts, or images.
- No network references at all. If you want an image, use an inline SVG or a CSS gradient. Never use <img src="..."> pointing at a URL.
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
