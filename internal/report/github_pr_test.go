package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// stubServer collects requests and returns scripted responses.
type stubServer struct {
	t  *testing.T
	mu sync.Mutex

	listReturns    []listReturn
	createCalls    []map[string]string
	editCalls      map[int]string
	listURL        string
	respMethod     string // last seen
	server         *httptest.Server
}

type listReturn struct {
	body []map[string]any
	link string
}

func newStub(t *testing.T) *stubServer {
	s := &stubServer{t: t, editCalls: map[int]string{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stubServer) URL() string { return s.server.URL }

func (s *stubServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
		s.t.Errorf("missing X-GitHub-Api-Version header")
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		s.t.Errorf("missing Bearer auth header")
	}

	switch {
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/") && strings.HasSuffix(r.URL.Path, "/comments"):
		s.listURL = r.URL.String()
		s.respMethod = "list"
		ret := listReturn{}
		if len(s.listReturns) > 0 {
			ret = s.listReturns[0]
			s.listReturns = s.listReturns[1:]
		}
		if ret.link != "" {
			w.Header().Set("Link", ret.link)
		}
		_ = json.NewEncoder(w).Encode(ret.body)

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
		s.respMethod = "create"
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		s.createCalls = append(s.createCalls, payload)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id": 999}`)

	case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/comments/"):
		s.respMethod = "edit"
		var payload struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		// Path shape: /repos/{owner}/{repo}/issues/comments/{id}
		parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
		id, _ := strconv.Atoi(parts[len(parts)-1])
		s.editCalls[id] = payload.Body
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":`+itoa(id)+`}`)

	default:
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

func newClientForTest(t *testing.T, url, body string, env func(string) string) *prClient {
	t.Helper()
	c, err := newPRClient(GitHubOptions{
		Token:      "secret-test-token",
		Repo:       "owner/repo",
		PRNumber:   42,
		apiBaseURL: url,
		httpClient: &http.Client{},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPRClient_StickyCreate_NoExistingComments(t *testing.T) {
	stub := newStub(t)
	stub.listReturns = []listReturn{{body: nil}} // empty page

	c := newClientForTest(t, stub.URL(), "", os.Getenv)
	if err := c.postSticky(context.Background(), "<!-- llm-lint-sticky -->\nbody\n<!-- llm-lint-sticky -->"); err != nil {
		t.Fatal(err)
	}
	if len(stub.createCalls) != 1 {
		t.Errorf("expected 1 POST; got %d", len(stub.createCalls))
	}
	if !strings.Contains(stub.createCalls[0]["body"], "<!-- llm-lint-sticky -->") {
		t.Errorf("body must contain marker; got %v", stub.createCalls[0])
	}
}

func TestPRClient_StickyEdit_FindsExisting(t *testing.T) {
	stub := newStub(t)
	stub.listReturns = []listReturn{{
		body: []map[string]any{
			{"id": 1, "body": "<!-- some-other-tool -->"},
			{"id": 99, "body": "<!-- llm-lint-sticky -->\nold body"},
		},
	}}

	c := newClientForTest(t, stub.URL(), "", os.Getenv)
	if err := c.postSticky(context.Background(), "<!-- llm-lint-sticky -->\nnew body\n<!-- llm-lint-sticky -->"); err != nil {
		t.Fatal(err)
	}
	if got, ok := stub.editCalls[99]; !ok {
		t.Errorf("expected PATCH on id 99; edits=%v", stub.editCalls)
	} else if !strings.Contains(got, "new body") {
		t.Errorf("edited body should contain new content; got %q", got)
	}
	if len(stub.createCalls) != 0 {
		t.Errorf("must not POST when sticky exists; got %d", len(stub.createCalls))
	}
}

func TestPRClient_AppendAlwaysPosts(t *testing.T) {
	stub := newStub(t)
	stub.listReturns = []listReturn{{
		body: []map[string]any{
			{"id": 99, "body": "<!-- llm-lint-sticky -->"},
		},
	}}
	c := newClientForTest(t, stub.URL(), "", os.Getenv)
	if err := c.postAppend(context.Background(), "<!-- llm-lint-sticky -->\nbody\n<!-- llm-lint-sticky -->"); err != nil {
		t.Fatal(err)
	}
	if len(stub.createCalls) != 1 {
		t.Errorf("append must POST; got %d posts", len(stub.createCalls))
	}
	if len(stub.editCalls) != 0 {
		t.Errorf("append must not PATCH; got %d edits", len(stub.editCalls))
	}
}

func TestPRClient_PaginationFollowsLink(t *testing.T) {
	stub := newStub(t)
	stub.listReturns = []listReturn{
		{
			body: []map[string]any{{"id": 1, "body": "x"}},
			// Pretend page 2 exists at our own server.
			link: `<` + "PAGE2" + `>; rel="next"`,
		},
		{
			body: []map[string]any{{"id": 77, "body": "<!-- llm-lint-sticky -->"}},
		},
	}
	// Replace PAGE2 with the actual URL of page 2 on the same server.
	stub.listReturns[0].link = strings.Replace(stub.listReturns[0].link, "PAGE2",
		stub.URL()+"/repos/owner/repo/issues/42/comments?page=2", 1)

	c := newClientForTest(t, stub.URL(), "", os.Getenv)
	if err := c.postSticky(context.Background(), "<!-- llm-lint-sticky -->\nx\n<!-- llm-lint-sticky -->"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stub.editCalls[77]; !ok {
		t.Errorf("expected PATCH on id 77 (found on page 2); edits=%v", stub.editCalls)
	}
}

func TestPRClient_TokenNeverInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	const tok = "verysecret-do-not-leak"
	c, err := newPRClient(GitHubOptions{
		Token:      tok,
		Repo:       "owner/repo",
		PRNumber:   42,
		apiBaseURL: srv.URL,
	}, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	gotErr := c.postSticky(context.Background(), "body")
	if gotErr == nil {
		t.Errorf("expected error on 401")
	}
	if strings.Contains(gotErr.Error(), tok) {
		t.Errorf("error must NOT contain token; got %q", gotErr.Error())
	}
	if !strings.Contains(gotErr.Error(), "401") {
		t.Errorf("error should mention status 401; got %q", gotErr.Error())
	}
}

func TestPRClient_NoTokenSkipsCleanly(t *testing.T) {
	_, err := newPRClient(GitHubOptions{Repo: "owner/repo", PRNumber: 1}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("expected error mentioning GITHUB_TOKEN; got %v", err)
	}
}

func TestPRClient_NoPRContext(t *testing.T) {
	_, err := newPRClient(GitHubOptions{Token: "x", Repo: "owner/repo"}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "PR number") {
		t.Errorf("expected error mentioning PR number; got %v", err)
	}
}

func TestPRClient_DetectsPRFromGITHUBREF(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "GITHUB_REF":
			return "refs/pull/77/merge"
		case "GITHUB_TOKEN":
			return "x"
		case "GITHUB_REPOSITORY":
			return "owner/repo"
		}
		return ""
	}
	c, err := newPRClient(GitHubOptions{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.pr != 77 {
		t.Errorf("expected pr=77; got %d", c.pr)
	}
}

func TestPRClient_DetectsPRFromEventPath(t *testing.T) {
	tmp := t.TempDir() + "/event.json"
	if err := os.WriteFile(tmp, []byte(`{"pull_request":{"number":88}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		switch k {
		case "GITHUB_EVENT_PATH":
			return tmp
		case "GITHUB_TOKEN":
			return "x"
		case "GITHUB_REPOSITORY":
			return "owner/repo"
		}
		return ""
	}
	c, err := newPRClient(GitHubOptions{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.pr != 88 {
		t.Errorf("expected pr=88; got %d", c.pr)
	}
}

// failingClient always errors.
type failingClient struct{}

func (failingClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("network down")
}

func TestPRClient_NetworkErrorReturnsError(t *testing.T) {
	c, err := newPRClient(GitHubOptions{
		Token:      "x",
		Repo:       "owner/repo",
		PRNumber:   1,
		apiBaseURL: "http://example.invalid",
		httpClient: failingClient{},
	}, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.postSticky(context.Background(), "x"); err == nil {
		t.Errorf("expected error on network failure")
	}
}
