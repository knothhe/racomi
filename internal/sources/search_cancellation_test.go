package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPendingHubRequestCanBeCanceledForNewSearch(t *testing.T) {
	for _, pending := range []struct {
		name string
		run  func(context.Context, *Client) error
	}{
		{"initial leaderboard", func(ctx context.Context, client *Client) error {
			_, err := client.ListSkills(ctx, 0)
			return err
		}},
		{"keyword search", func(ctx context.Context, client *Client) error {
			_, err := client.Search(ctx, "previous")
			return err
		}},
	} {
		t.Run(pending.name, func(t *testing.T) {
			started := make(chan struct{})
			canceled := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/search" && r.URL.Query().Get("q") == "replacement" {
					fmt.Fprint(w, `{"skills":[{"id":"owner/repo/replacement","name":"replacement","installs":1}]}`)
					return
				}
				close(started)
				select {
				case <-r.Context().Done():
					close(canceled)
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			client := New(t.TempDir())
			client.searchBase = server.URL
			ctx, timeout := context.WithTimeout(t.Context(), 5*time.Second)
			defer timeout()
			previousCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			previousResult := make(chan error, 1)
			go func() { previousResult <- pending.run(previousCtx, client) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("initial request did not reach the server")
			}

			cancel()
			results, err := client.Search(ctx, "replacement")
			if err != nil || len(results) != 1 || results[0].Name != "replacement" {
				t.Fatalf("replacement search failed: %#v, %v", results, err)
			}
			select {
			case err := <-previousResult:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("previous request did not report cancellation: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("previous request did not stop after cancellation")
			}
			select {
			case <-canceled:
			case <-ctx.Done():
				t.Fatal("cancellation did not reach the pending HTTP request")
			}
		})
	}
}
