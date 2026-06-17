// Package googlenews implements news.Provider against Google News RSS, which is
// free and needs no API key.
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
	"time"

	"github.com/oddvice/api/internal/news"
)

// Client fetches and parses a Google News RSS feed.
type Client struct {
	httpClient *http.Client
	feedURL    string
	limit      int
}

// New builds a Client. A nil httpClient gets a default one; limit <= 0 disables
// the cap.
func New(feedURL string, limit int, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{httpClient: httpClient, feedURL: feedURL, limit: limit}
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
		URL  string `xml:"url,attr"`
	} `xml:"source"`
}

// Latest fetches the feed and maps it to domain articles.
func (c *Client) Latest(ctx context.Context) ([]news.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
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
	return articles, nil
}

func (it rssItem) toArticle() news.Article {
	return news.Article{
		ID:          articleID(it.GUID, it.Link),
		Title:       strings.TrimSpace(html.UnescapeString(it.Title)),
		Link:        strings.TrimSpace(it.Link),
		Source:      strings.TrimSpace(it.Source.Name),
		Image:       logoURL(it.Source.URL),
		Summary:     summarize(it.Description),
		PublishedAt: parsePubDate(it.PubDate),
	}
}

// logoURL derives a publisher logo from the source homepage via Google's
// favicon service, which always returns an image (no broken thumbnails).
func logoURL(sourceURL string) string {
	host := strings.TrimSpace(sourceURL)
	if host == "" {
		return ""
	}
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		host = u.Host
	}
	return "https://www.google.com/s2/favicons?sz=128&domain=" + url.QueryEscape(host)
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
