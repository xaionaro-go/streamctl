package oauthhandler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/xaionaro-go/streamctl/pkg/observability"
)

type OAuthHandlerArgument struct {
	AuthURL    string
	ListenPort uint16
	ExchangeFn func(code string) error
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
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	codeCh, _, err := NewCodeReceiver(ctx, arg.ListenPort)
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

func NewCodeReceiver(
	ctx context.Context,
	listenPort uint16,
) (chan string, uint16, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		return nil, 0, err
	}
	codeCh := make(chan string)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.FormValue("code")
			codeCh <- code
			w.Header().Set("Content-Type", "text/plain")
			if code == "" {
				fmt.Fprintf(w, "No code received :(\r\n\r\nYou can close this browser window.")
				return
			}
			fmt.Fprintf(w, "Received code: %v\r\n\r\nYou can now safely close this browser window.", code)
		}),
	}

	observability.Go(ctx, func() {
		<-ctx.Done()
		listener.Close()
		srv.Close()
		close(codeCh)
	})

	observability.Go(ctx, func() {
		srv.Serve(listener)
	})

	return codeCh, uint16(listener.Addr().(*net.TCPAddr).Port), nil
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
