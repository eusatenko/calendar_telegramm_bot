// Command google-auth performs a one-time local OAuth authorization and prints
// the refresh token that must be copied to the production .env file.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const redirectURI = "http://127.0.0.1:53682/callback"

func main() {
	clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		fmt.Fprintln(os.Stderr, "GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are required")
		os.Exit(1)
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		panic(err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	listenAddress := os.Getenv("GOOGLE_OAUTH_LISTEN_ADDRESS")
	if listenAddress == "" {
		listenAddress = "127.0.0.1:53682"
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot listen for OAuth callback:", err)
		os.Exit(1)
	}
	defer listener.Close()

	result := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" || r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid OAuth callback", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			http.Error(w, html.EscapeString(oauthErr), http.StatusBadRequest)
			result <- ""
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Authorization code is missing", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintln(w, "Авторизация завершена. Можно закрыть вкладку.")
		result <- code
	})

	authQuery := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"https://www.googleapis.com/auth/calendar.events"},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	fmt.Println("Откройте URL в браузере и разрешите доступ к событиям календаря:")
	fmt.Println()
	fmt.Println("https://accounts.google.com/o/oauth2/v2/auth?" + authQuery.Encode())

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			result <- ""
		}
	}()

	var code string
	select {
	case code = <-result:
	case <-time.After(5 * time.Minute):
		fmt.Fprintln(os.Stderr, "authorization timed out")
		os.Exit(1)
	}
	_ = server.Shutdown(context.Background())
	if code == "" {
		fmt.Fprintln(os.Stderr, "authorization failed")
		os.Exit(1)
	}

	values := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
	}
	request, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		fmt.Fprintln(os.Stderr, "token exchange failed:", err)
		os.Exit(1)
	}
	defer response.Body.Close()
	var token struct {
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil || response.StatusCode < 200 || response.StatusCode >= 300 || token.RefreshToken == "" {
		fmt.Fprintf(os.Stderr, "token exchange failed: HTTP %d, %s\n", response.StatusCode, token.Error)
		os.Exit(1)
	}
	fmt.Println("\nGOOGLE_OAUTH_REFRESH_TOKEN=" + token.RefreshToken)
}
