package gdoc

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// oauth2Client returns an HTTP client that adds (and refreshes) the service account's
// access token on every request.
func oauth2Client(ctx context.Context, creds *google.Credentials) *http.Client {
	c := oauth2.NewClient(ctx, creds.TokenSource)
	c.Timeout = 60 * time.Second
	return c
}
