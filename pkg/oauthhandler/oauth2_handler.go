package oauthhandler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type OAuth2Handler struct {
	authURL      string
	exchangeFn   func(code string) error
	receiverAddr string
}

func NewOAuth2Handler(
	authURL string,
	exchangeFn func(code string) error,
	receiverAddr string,
) *OAuth2Handler {
	return &OAuth2Handler{
		authURL:      authURL,
		exchangeFn:   exchangeFn,
		receiverAddr: receiverAddr,
	}
}

// it is guaranteed exchangeFn was called if error is nil.
func (h *OAuth2Handler) Handle(ctx context.Context) error {
	if h.receiverAddr != "" {
		err := h.handleViaBrowser()
		if err == nil {
			return nil
		}
		logger.Errorf(ctx, "unable to authenticate automatically: %v", err)
	}
	return h.handleViaCLI()
}

func (h *OAuth2Handler) handleViaCLI() error {
	fmt.Printf(
		"It is required to get an oauth2 token. "+
			"Please open the link below in the browser:\n\n\t%s\n\n",
		h.authURL,
	)

	fmt.Printf("Enter the code: ")
	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}
	return h.exchangeFn(code)
}

func (h *OAuth2Handler) handleViaBrowser() error {
	codeCh, err := h.newCodeReceiver()
	if err != nil {
		return err
	}

	err = launchBrowser(h.authURL)
	if err != nil {
		return err
	}

	fmt.Printf("Your browser has been launched (URL: %s).\nPlease approve the permissions.\n", h.authURL)

	// Wait for the web server to get the code.
	code := <-codeCh
	return h.exchangeFn(code)
}

func (h *OAuth2Handler) newCodeReceiver() (codeCh chan string, err error) {
	// this function was mostly borrowed from https://developers.google.com/youtube/v3/code_samples/go#authorize_a_request
	listener, err := net.Listen("tcp", h.receiverAddr)
	if err != nil {
		return nil, err
	}
	codeCh = make(chan string)

	go http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.FormValue("code")
		codeCh <- code
		listener.Close()
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Received code: %v\r\nYou can now safely close this browser window.", code)
	}))

	return codeCh, nil
}

func launchBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return fmt.Errorf("unsupported platform: <%s>", runtime.GOOS)
}
