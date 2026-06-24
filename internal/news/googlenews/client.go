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
	"io"
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

	// Per-article-URL enrichment (og:image / og:description), shared across
	// languages and filled in the background so the response never blocks.
	enrichMu       sync.Mutex
	enrichCache    map[string]enrichEntry
	enrichInflight map[string]bool
}

type cachedFeed struct {
	articles []news.Article
	at       time.Time
}

type enrichEntry struct {
	image string
	desc  string
	at    time.Time
}

// enrichTTL is how long a fetched (or failed) enrichment is trusted.
const enrichTTL = 12 * time.Hour

// New builds a Client. A nil httpClient gets a default one; limit <= 0 disables
// the cap.
func New(limit int, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		httpClient:     httpClient,
		limit:          limit,
		cacheTTL:       5 * time.Minute,
		cache:          make(map[string]cachedFeed),
		enrichCache:    make(map[string]enrichEntry),
		enrichInflight: make(map[string]bool),
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
		c.ensureEnriched(cached.articles)
		return c.withEnrich(cached.articles), nil
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

	c.ensureEnriched(articles)
	return c.withEnrich(articles), nil
}

// withEnrich returns a copy of articles with any cached og:image / og:description
// applied. Cheap (map lookups); safe to call on every request.
func (c *Client) withEnrich(articles []news.Article) []news.Article {
	out := make([]news.Article, len(articles))
	copy(out, articles)
	c.enrichMu.Lock()
	for i := range out {
		if e, ok := c.enrichCache[out[i].Link]; ok {
			if e.image != "" {
				out[i].Image = e.image
			}
			if len(e.desc) > len(out[i].Summary) {
				out[i].Summary = e.desc
			}
		}
	}
	c.enrichMu.Unlock()
	return out
}

// ensureEnriched fetches og:image / og:description for any article URL we haven't
// resolved (or whose entry is stale), in the background, concurrency-limited. It
// never blocks the caller; results land in enrichCache for the next request.
func (c *Client) ensureEnriched(articles []news.Article) {
	var todo []string
	c.enrichMu.Lock()
	for _, a := range articles {
		if a.Link == "" || c.enrichInflight[a.Link] {
			continue
		}
		if e, ok := c.enrichCache[a.Link]; ok && time.Since(e.at) < enrichTTL {
			continue
		}
		c.enrichInflight[a.Link] = true
		todo = append(todo, a.Link)
	}
	c.enrichMu.Unlock()
	if len(todo) == 0 {
		return
	}

	go func() {
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, link := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func(link string) {
				defer wg.Done()
				defer func() { <-sem }()
				img, desc := c.fetchMeta(link)
				c.enrichMu.Lock()
				c.enrichCache[link] = enrichEntry{image: img, desc: desc, at: time.Now()}
				delete(c.enrichInflight, link)
				c.enrichMu.Unlock()
			}(link)
		}
		wg.Wait()
	}()
}

var (
	metaTagRe = regexp.MustCompile(`(?i)<meta[^>]+>`)
	contentRe = regexp.MustCompile(`(?i)content\s*=\s*["']([^"']+)["']`)
)

// fetchMeta best-effort fetches an article page and extracts og:image (or
// twitter:image) and og:description. Returns ("","") on any failure/timeout.
func (c *Client) fetchMeta(link string) (image, desc string) {
	// Google News article pages expose an og:image (Google's thumbnail) and
	// og:description; bypass the consent interstitial with ucbcb=1.
	if strings.Contains(link, "news.google.") && !strings.Contains(link, "ucbcb=") {
		if strings.Contains(link, "?") {
			link += "&ucbcb=1"
		} else {
			link += "?ucbcb=1"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", ""
	}
	page := string(body)
	for _, tag := range metaTagRe.FindAllString(page, -1) {
		low := strings.ToLower(tag)
		m := contentRe.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		val := html.UnescapeString(strings.TrimSpace(m[1]))
		if image == "" && (strings.Contains(low, "og:image") || strings.Contains(low, "twitter:image")) {
			if strings.HasPrefix(val, "http") {
				image = val
			}
		}
		if desc == "" && strings.Contains(low, "og:description") {
			desc = val
		}
	}
	return image, desc
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
