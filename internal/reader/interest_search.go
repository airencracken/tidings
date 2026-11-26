package reader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/publicsuffix"
)

const (
	maxInterestLength   = 200
	maxSearchResponse   = 1 << 20
	maxInterestSites    = 8
	maxFeedsPerSite     = 2
	interestSearchLimit = 10 * time.Second
)

type interestSearch struct {
	endpoint string
	client   *http.Client
	slots    chan struct{}
}

type interestResult struct {
	Title, URL, Publisher string
	Headlines             []Entry
	FollowedID            int64
}

func newInterestSearch(raw string) (*interestSearch, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("TIDINGS_SEARXNG_URL must be an HTTP or HTTPS search endpoint without credentials, query, or fragment")
	}
	return &interestSearch{
		endpoint: u.String(),
		client: &http.Client{
			Timeout: 4 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		slots: make(chan struct{}, 2),
	}, nil
}

func readInterest(values []string) (string, error) {
	if len(values) == 1 {
		query := strings.TrimSpace(values[0])
		if query != "" && utf8.ValidString(query) && utf8.RuneCountInString(query) <= maxInterestLength &&
			!strings.ContainsFunc(query, unicode.IsControl) {
			return query, nil
		}
	}
	return "", errors.New("Enter an interest of up to 200 characters on one line")
}

func publisherDomain(host string) string {
	host = strings.TrimSuffix(host, ".")
	if domain, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return domain
	}
	return host
}

func (s *interestSearch) candidates(ctx context.Context, query string) ([]string, error) {
	values := url.Values{"q": {query + " RSS"}, "format": {"json"}, "language": {"all"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("search provider returned an unsuccessful status")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchResponse+1))
	var payload struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
	}
	if err != nil || len(body) > maxSearchResponse || json.Unmarshal(body, &payload) != nil || payload.Results == nil {
		return nil, errors.New("invalid search provider response")
	}
	addresses := make([]string, 0, maxInterestSites)
	seen := map[string]bool{}
	for _, item := range payload.Results {
		address, err := WebURL(item.URL)
		if err != nil {
			continue
		}
		u, _ := url.Parse(address)
		domain := publisherDomain(u.Hostname())
		if seen[domain] {
			continue
		}
		seen[domain] = true
		addresses = append(addresses, address)
		if len(addresses) == maxInterestSites {
			break
		}
	}
	return addresses, nil
}

func (f *Fetcher) interestFeed(ctx context.Context, address string) interestResult {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	u, _ := url.Parse(address)
	home := url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
	feeds, err := f.Discover(ctx, address)
	if err != nil && home.String() != address {
		// Article pages sometimes omit the feed links found on the home page.
		feeds, _ = f.Discover(ctx, home.String())
	}
	feedURLs := make([]string, 0, maxFeedsPerSite+1)
	for _, feed := range feeds {
		if commentFeedAddress(feed.URL) {
			continue
		}
		feedURLs = append(feedURLs, feed.URL)
		if len(feedURLs) == maxFeedsPerSite {
			break
		}
	}
	home.Path = "/feed/"
	if !slices.Contains(feedURLs, home.String()) {
		feedURLs = append(feedURLs, home.String())
	}
	for _, address := range feedURLs {
		result, err := f.Fetch(ctx, address, "", "")
		if err != nil || len(result.Feed.Entries) == 0 {
			continue
		}
		if commentFeedAddress(result.Address) || strings.HasPrefix(strings.ToLower(result.Feed.Title), "comments for ") {
			continue
		}
		u, _ := url.Parse(result.Address)
		headlines := make([]Entry, 0, 3)
		for _, item := range recentHeadlines(result.Feed.Entries, 3) {
			headlines = append(headlines, Entry{Title: item.Title, URL: item.URL, Published: item.Published})
		}
		return interestResult{
			Title: result.Feed.Title, URL: result.Address, Publisher: publisherDomain(u.Hostname()),
			Headlines: headlines,
		}
	}
	return interestResult{}
}

func commentFeedAddress(address string) bool {
	u, _ := url.Parse(address)
	path := "/" + strings.Trim(u.Path, "/") + "/"
	return strings.Contains(path, "/comments/feed/") || strings.Contains(path, "/feeds/comments/") ||
		strings.HasPrefix(u.Query().Get("feed"), "comments-")
}

func (a *App) searchInterest(w http.ResponseWriter, r *http.Request) {
	group := scope(r)
	if group && !current(r).User.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	if a.interestSearch == nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	query, err := readInterest(r.PostForm["interest"])
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	p := page{Title: "Find feeds by interest", Kind: "interest-results", GroupScope: group, InterestQuery: query}
	select {
	case a.interestSearch.slots <- struct{}{}:
		defer func() { <-a.interestSearch.slots }()
	default:
		p.Error = "Feed search is busy. Try again in a moment."
		a.render(w, r, p, http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), interestSearchLimit)
	defer cancel()
	addresses, err := a.interestSearch.candidates(ctx, query)
	if err != nil {
		p.Error = "The search service is unavailable. Try again or add a feed by website address."
		a.render(w, r, p, http.StatusBadGateway)
		return
	}
	feeds, err := a.Store.Feeds(r.Context(), current(r).User.ID, group)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.InterestResults = a.resolveInterestFeeds(ctx, addresses, feeds)
	p.InterestCanFollow = slices.ContainsFunc(p.InterestResults, func(result interestResult) bool { return result.FollowedID == 0 })
	a.render(w, r, p, http.StatusOK)
}

func (a *App) followInterest(w http.ResponseWriter, r *http.Request) {
	u, group := current(r).User, scope(r)
	if group && !u.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	if a.interestSearch == nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	addresses := r.PostForm["url"]
	if len(addresses) == 0 || len(addresses) > maxInterestSites {
		a.bad(w, r, "Choose one or more feeds from the results")
		return
	}
	feeds := make([]Feed, 0, len(addresses))
	for _, address := range addresses {
		feeds = append(feeds, Feed{URL: address})
	}
	if err := a.Store.Subscribe(r.Context(), u.ID, feeds, group); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	a.StartRefresh(u.ID, false)
	values := url.Values{"notice": {"feeds-followed"}}
	if group {
		values.Set("scope", "household")
	}
	target := url.URL{Path: "/sources", RawQuery: values.Encode()}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (a *App) resolveInterestFeeds(ctx context.Context, addresses []string, followed []Feed) []interestResult {
	results := make([]interestResult, len(addresses))
	var wg sync.WaitGroup
	for i, address := range addresses {
		wg.Go(func() { results[i] = a.Fetcher.interestFeed(ctx, address) })
	}
	wg.Wait()
	followedIDs := map[string]int64{}
	for _, feed := range followed {
		followedIDs[feed.URL] = feed.ID
	}
	seen := map[string]bool{}
	found := results[:0]
	for _, result := range results {
		if result.URL == "" || seen[result.Publisher] {
			continue
		}
		seen[result.Publisher] = true
		result.FollowedID = followedIDs[result.URL]
		found = append(found, result)
	}
	return found
}

func interestTags(feeds []Feed) []string {
	seen := map[string]bool{}
	var tags []string
	for _, feed := range feeds {
		for _, tag := range feed.Tags {
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	sort.Strings(tags)
	return tags
}
