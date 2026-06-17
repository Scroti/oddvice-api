package googlenews

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleFeed = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>Test</title>
  <item>
    <title>FIFA anunță decizia &amp; programul</title>
    <link>https://news.google.com/rss/articles/ABC123</link>
    <guid>tag:news.google.com,ABC123</guid>
    <pubDate>Wed, 17 Jun 2026 13:47:00 GMT</pubDate>
    <description>&lt;a href="x"&gt;Snippet&lt;/a&gt; despre &lt;b&gt;meci&lt;/b&gt;</description>
    <source url="https://digisport.ro">Digi Sport</source>
  </item>
  <item>
    <title>Al doilea articol</title>
    <link>https://news.google.com/rss/articles/DEF456</link>
    <guid>tag:news.google.com,DEF456</guid>
    <pubDate>Tue, 16 Jun 2026 09:00:00 GMT</pubDate>
    <description>text simplu</description>
    <source url="https://gsp.ro">GSP</source>
  </item>
</channel></rss>`

func TestLatest_ParsesAndMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	c := New(srv.URL, 0, srv.Client())
	articles, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}

	a := articles[0]
	if a.Title != "FIFA anunță decizia & programul" {
		t.Errorf("title not unescaped: %q", a.Title)
	}
	if a.Source != "Digi Sport" {
		t.Errorf("source wrong: %q", a.Source)
	}
	if a.Summary != "Snippet despre meci" {
		t.Errorf("summary not stripped to plain text: %q", a.Summary)
	}
	if a.ID == "" {
		t.Error("expected a non-empty id")
	}
	if a.PublishedAt == nil || a.PublishedAt.Year() != 2026 {
		t.Errorf("pubDate not parsed: %v", a.PublishedAt)
	}
}

func TestLatest_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	c := New(srv.URL, 1, srv.Client())
	articles, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected limit of 1, got %d", len(articles))
	}
}
