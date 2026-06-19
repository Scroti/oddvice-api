// Package googlenews implements news.Provider against Google News RSS, which is
// free and needs no API key. Feeds are localized per app language (query +
// hl/gl/ceid), so English users get English news, French users French, etc.
package googlenews

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oddvice/api/internal/news"
)

// feedParams configures one language's Google News search feed.
type feedParams struct {
	query string
	hl    string // interface language, e.g. "fr"
	gl    string // country, e.g. "FR"
	ceid  string // "<country>:<lang>", e.g. "FR:fr"
}

// feeds maps an app locale to a localized World Cup 2026 news feed.
var feeds = map[string]feedParams{
	"en": {"World Cup 2026", "en-US", "US", "US:en"},
	"ro": {"Cupa Mondială 2026", "ro", "RO", "RO:ro"},
	"de": {"Fußball WM 2026", "de", "DE", "DE:de"},
	"fr": {"Coupe du Monde 2026", "fr", "FR", "FR:fr"},
	"es": {"Mundial 2026", "es", "ES", "ES:es"},
	"it": {"Mondiali 2026", "it", "IT", "IT:it"},
	"nl": {"WK voetbal 2026", "nl", "NL", "NL:nl"},
	"pl": {"Mistrzostwa Świata 2026", "pl", "PL", "PL:pl"},
	"cs": {"Mistrovství světa 2026", "cs", "CZ", "CZ:cs"},
}

// feedURL builds the Google News RSS search URL for a language (falls back to en).
func feedURL(lang string) string {
	fp, ok := feeds[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		fp = feeds["en"]
	}
	return fmt.Sprintf(
		"https://news.google.com/rss/search?q=%s&hl=%s&gl=%s&ceid=%s",
		url.QueryEscape(fp.query), fp.hl, fp.gl, url.QueryEscape(fp.ceid),
	)
}

// Client fetches and parses localized Google News RSS feeds, cached per language.
type Client struct {
	httpClient  *http.Client
	limit       int
	cacheTTL    time.Duration
	overrideURL string // test seam: when set, used instead of the localized URL

	mu    sync.Mutex
	cache map[string]cachedFeed
}

type cachedFeed struct {
	articles []news.Article
	at       time.Time
}

// New builds a Client. A nil httpClient gets a default one; limit <= 0 disables
// the cap.
func New(limit int, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		limit:      limit,
		cacheTTL:   5 * time.Minute,
		cache:      make(map[string]cachedFeed),
	}
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
	Source      struct {
		Name string `xml:",chardata"`
	} `xml:"source"`
}

// Latest fetches the localized feed for lang and maps it to domain articles.
// Results are cached per language; stale cache is served if a refresh fails.
func (c *Client) Latest(ctx context.Context, lang string) ([]news.Article, error) {
	key := strings.ToLower(strings.TrimSpace(lang))
	if _, ok := feeds[key]; !ok {
		key = "en"
	}

	c.mu.Lock()
	cached, has := c.cache[key]
	fresh := has && time.Since(cached.at) < c.cacheTTL
	c.mu.Unlock()
	if fresh {
		return cached.articles, nil
	}

	target := feedURL(key)
	if c.overrideURL != "" {
		target = c.overrideURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if has {
			return cached.articles, nil // serve stale on failure
		}
		return nil, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if has {
			return cached.articles, nil
		}
		return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		if has {
			return cached.articles, nil
		}
		return nil, fmt.Errorf("decode feed: %w", err)
	}

	items := feed.Channel.Items
	if c.limit > 0 && len(items) > c.limit {
		items = items[:c.limit]
	}
	articles := make([]news.Article, 0, len(items))
	for _, it := range items {
		articles = append(articles, it.toArticle())
	}

	c.mu.Lock()
	c.cache[key] = cachedFeed{articles: articles, at: time.Now()}
	c.mu.Unlock()
	return articles, nil
}

func (it rssItem) toArticle() news.Article {
	return news.Article{
		ID:          articleID(it.GUID, it.Link),
		Title:       strings.TrimSpace(html.UnescapeString(it.Title)),
		Link:        strings.TrimSpace(it.Link),
		Source:      strings.TrimSpace(it.Source.Name),
		Summary:     summarize(it.Description),
		PublishedAt: parsePubDate(it.PubDate),
	}
}

// articleID is a stable, URL-safe id derived from the guid (or link).
func articleID(guid, link string) string {
	seed := strings.TrimSpace(guid)
	if seed == "" {
		seed = strings.TrimSpace(link)
	}
	sum := sha1.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])[:16]
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

// summarize strips HTML tags/entities from an RSS description into plain text.
func summarize(desc string) string {
	text := tagRe.ReplaceAllString(desc, " ")
	text = html.UnescapeString(text)
	text = strings.Join(strings.Fields(text), " ") // collapse whitespace
	return text
}

func parsePubDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}
