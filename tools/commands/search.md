Search the web via Brave Search (or DuckDuckGo Lite as fallback).

Flags:
  --max N    Maximum results (default 10, max 20)

Returns title, URL, and snippet for each result.

Set BRAVE_API_KEY for Brave Search (recommended).
Falls back to DuckDuckGo Lite if no API key is set.

Examples:
  temenos search "golang context timeout patterns"
  temenos search "RFC 7231 HTTP semantics" --max 5
