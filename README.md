# Hallucination Search

A web search engine. Kind of.

Every result is fabricated by a large language model, and every destination page is fabricated in full by a large language model, too. Nothing you find here is real, and that is the whole joke.

Made with ✨sparkles✨ by [maragu](https://www.maragu.dev/): independent software consulting for cloud-native Go apps & AI engineering.

## How it works

- The homepage is a single search input.
- Submitting a query lands on `/?q=...`, which shows ten Google-style result cards. Results are fabricated by Claude Sonnet and cached in SQLite so the same query renders instantly on refresh.
- Cards fill in progressively via Server-Sent Events driven by Datastar signals.
- Clicking a result blocks for up to two minutes while Claude Opus fabricates a full standalone HTML document, which is then cached in SQLite and served raw.

## Running

Set your Anthropic API key, then let the watch loop do the rest:

```sh
cp .env.example .env
echo "ANTHROPIC_API_KEY=sk-ant-..." >> .env   # replace with your key
make watch
```

The app writes logs to `app.log`, rebuilds on file changes, and listens on the address configured by `SERVER_ADDRESS` (default `:8080`).

Inspect the cache directly with `sqlite3 app.db` (remember `pragma foreign_keys = 1;`).

[Contact me at markus@maragu.dk](mailto:markus@maragu.dk) for consulting work, or perhaps an invoice to support this project?
