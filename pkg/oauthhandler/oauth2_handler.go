package oauthhandler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type OAuthHandlerArgument struct {
	AuthURL     string
	RedirectURL string
	ExchangeFn  func(code string) error
}

// it is guaranteed exchangeFn was called if error is nil.
func OAuth2Handler(ctx context.Context, arg OAuthHandlerArgument) error {
	if arg.RedirectURL != "" {
		err := OAuth2HandlerViaBrowser(ctx, arg)
		if err == nil {
			return nil
		}
		logger.Errorf(ctx, "unable to authenticate automatically: %v", err)
	}
	return OAuth2HandlerViaCLI(ctx, arg)
}

func OAuth2HandlerViaCLI(ctx context.Context, arg OAuthHandlerArgument) error {
	fmt.Printf(
		"It is required to get an oauth2 token. "+
			"Please open the link below in the browser:\n\n\t%s\n\n",
		arg.AuthURL,
	)

	fmt.Printf("Enter the code: ")
	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}
	return arg.ExchangeFn(code)
}

func OAuth2HandlerViaBrowser(ctx context.Context, arg OAuthHandlerArgument) error {
	codeCh, err := NewCodeReceiver(arg.RedirectURL)
	if err != nil {
		return err
	}

	err = LaunchBrowser(arg.AuthURL)
	if err != nil {
		return err
	}

	fmt.Printf("Your browser has been launched (URL: %s).\nPlease approve the permissions.\n", arg.AuthURL)

	// Wait for the web server to get the code.
	code := <-codeCh
	return arg.ExchangeFn(code)
}

func NewCodeReceiver(redirectURL string) (codeCh chan string, err error) {
	urlParsed, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", redirectURL, err)
	}

	// this function was mostly borrowed from https://developers.google.com/youtube/v3/code_samples/go#authorize_a_request
	listener, err := net.Listen("tcp", urlParsed.Host)
	if err != nil {
		return nil, err
	}
	codeCh = make(chan string)

	go http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.FormValue("code")
		codeCh <- code
		listener.Close()
		w.Header().Set("Content-Type", "text/plain")
		if code == "" {
			fmt.Fprintf(w, "No code received :(\r\n\r\nYou can close this browser window.")
			return
		}
		fmt.Fprintf(w, "Received code: %v\r\n\r\nYou can now safely close this browser window.", code)
	}))

	return codeCh, nil
}

func LaunchBrowser(url string) error {
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
