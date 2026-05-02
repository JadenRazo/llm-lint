package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// httpDoer is a minimal interface for *http.Client so tests can stub.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

const (
	defaultGitHubAPIBase = "https://api.github.com"
	githubAPIVersion     = "2022-11-28"
)

type prClient struct {
	http   httpDoer
	base   string
	token  string
	repo   string // "owner/repo"
	pr     int
	marker string
}

// newPRClient resolves config from opts + env. Returns an error (not a
// panic) when essentials are missing; the caller logs and skips.
func newPRClient(opts GitHubOptions, env func(string) string) (*prClient, error) {
	if env == nil {
		env = os.Getenv
	}
	token := opts.Token
	if token == "" {
		token = env("GITHUB_TOKEN")
	}
	if token == "" {
		return nil, errors.New("no GITHUB_TOKEN")
	}
	repo := opts.Repo
	if repo == "" {
		repo = env("GITHUB_REPOSITORY")
	}
	if repo == "" {
		return nil, errors.New("no GITHUB_REPOSITORY")
	}
	pr := opts.PRNumber
	if pr == 0 {
		pr = detectPRNumber(env)
	}
	if pr == 0 {
		return nil, errors.New("no PR number (not in a pull_request context)")
	}
	base := opts.apiBaseURL
	if base == "" {
		base = env("GITHUB_API_URL")
	}
	if base == "" {
		base = defaultGitHubAPIBase
	}
	httpC := opts.httpClient
	if httpC == nil {
		httpC = &http.Client{Timeout: 15 * time.Second}
	}
	return &prClient{
		http:   httpC,
		base:   strings.TrimRight(base, "/"),
		token:  token,
		repo:   repo,
		pr:     pr,
		marker: stickyMarker,
	}, nil
}

var prRefRe = regexp.MustCompile(`^refs/pull/(\d+)/(merge|head)$`)

// detectPRNumber tries GITHUB_REF first, then $GITHUB_EVENT_PATH JSON.
func detectPRNumber(env func(string) string) int {
	if ref := env("GITHUB_REF"); ref != "" {
		m := prRefRe.FindStringSubmatch(ref)
		if len(m) == 3 {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	if path := env("GITHUB_EVENT_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var ev struct {
				PullRequest struct {
					Number int `json:"number"`
				} `json:"pull_request"`
				Number int `json:"number"`
			}
			if err := json.Unmarshal(data, &ev); err == nil {
				if ev.PullRequest.Number > 0 {
					return ev.PullRequest.Number
				}
				if ev.Number > 0 {
					return ev.Number
				}
			}
		}
	}
	return 0
}

func (c *prClient) postSticky(ctx context.Context, body string) error {
	id, err := c.findStickyComment(ctx)
	if err != nil {
		return err
	}
	if id > 0 {
		return c.editComment(ctx, id, body)
	}
	return c.createComment(ctx, body)
}

func (c *prClient) postAppend(ctx context.Context, body string) error {
	return c.createComment(ctx, body)
}

// listURL returns the comments list endpoint with paging.
func (c *prClient) listURL(page string) string {
	if page != "" {
		return page
	}
	return fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100", c.base, c.repo, c.pr)
}

// findStickyComment paginates through PR comments looking for the marker.
// Returns the comment ID (0 if none found).
func (c *prClient) findStickyComment(ctx context.Context) (int64, error) {
	pageURL := c.listURL("")
	for pageURL != "" {
		req, err := c.newReq(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return 0, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, c.wrapErr(http.MethodGet, pageURL, 0, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return 0, c.wrapErr(http.MethodGet, pageURL, resp.StatusCode, nil)
		}
		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(body, &comments); err != nil {
			return 0, fmt.Errorf("decode comments: %w", err)
		}
		for _, cm := range comments {
			if strings.Contains(cm.Body, c.marker) {
				return cm.ID, nil
			}
		}
		pageURL = parseNextLink(resp.Header.Get("Link"))
	}
	return 0, nil
}

func (c *prClient) createComment(ctx context.Context, body string) error {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.base, c.repo, c.pr)
	return c.bodyRequest(ctx, http.MethodPost, url, body)
}

func (c *prClient) editComment(ctx context.Context, id int64, body string) error {
	url := fmt.Sprintf("%s/repos/%s/issues/comments/%d", c.base, c.repo, id)
	return c.bodyRequest(ctx, http.MethodPatch, url, body)
}

func (c *prClient) bodyRequest(ctx context.Context, method, urlStr, body string) error {
	payload, _ := json.Marshal(map[string]string{"body": body})
	req, err := c.newReq(ctx, method, urlStr, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.wrapErr(method, urlStr, 0, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return c.wrapErr(method, urlStr, resp.StatusCode, nil)
	}
	return nil
}

func (c *prClient) newReq(ctx context.Context, method, urlStr string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "llm-lint")
	return req, nil
}

// wrapErr produces a sanitized error string. Never includes the token,
// request body, or response body — those can leak the bearer token across
// log boundaries.
func (c *prClient) wrapErr(method, urlStr string, status int, cause error) error {
	path := redactURL(urlStr, c.base)
	if status > 0 {
		return fmt.Errorf("github API %s %s: %d", method, path, status)
	}
	// Network-level error. Keep cause but ensure it doesn't expose the token.
	return fmt.Errorf("github API %s %s: %v", method, path, sanitizeErr(cause, c.token))
}

func redactURL(urlStr, base string) string {
	if u, err := url.Parse(urlStr); err == nil {
		u.User = nil
		s := u.String()
		if strings.HasPrefix(s, base) {
			return strings.TrimPrefix(s, base)
		}
		return s
	}
	return strings.TrimPrefix(urlStr, base)
}

func sanitizeErr(err error, token string) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if token != "" && strings.Contains(s, token) {
		s = strings.ReplaceAll(s, token, "[redacted]")
		return errors.New(s)
	}
	return err
}

var nextLinkRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func parseNextLink(header string) string {
	m := nextLinkRe.FindStringSubmatch(header)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
