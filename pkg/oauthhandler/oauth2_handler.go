package oauthhandler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type OAuthHandlerArgument struct {
	AuthURL    string
	ListenPort uint16
	ExchangeFn func(ctx context.Context, code string) error
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
	return arg.ExchangeFn(ctx, code)
}

func OAuth2HandlerViaBrowser(ctx context.Context, arg OAuthHandlerArgument) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	codeCh, _, err := NewCodeReceiver(ctx, arg.ListenPort)
	if err != nil {
		return err
	}

	err = LaunchBrowser(ctx, arg.AuthURL)
	if err != nil {
		return err
	}

	fmt.Printf(
		"Your browser has been launched (URL: %s).\nPlease approve the permissions.\n",
		arg.AuthURL,
	)

	// Wait for the web server to get the code.
	code := <-codeCh
	return arg.ExchangeFn(ctx, code)
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
			fmt.Fprintf(
				w,
				"Received code: %v\r\n\r\nYou can now safely close this browser window.",
				code,
			)
		}),
	}

	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		listener.Close()
		srv.Close()
		close(codeCh)
	})

	observability.Go(ctx, func(ctx context.Context) {
		srv.Serve(listener)
	})

	return codeCh, uint16(listener.Addr().(*net.TCPAddr).Port), nil
}

func LaunchBrowser(
	ctx context.Context,
	url string,
) error {
	var args []string
	switch runtime.GOOS {
	case "darwin":
		args = []string{"open"}
	case "linux":
		args = []string{"xdg-open"}
	case "windows":
		args = []string{"rundll32", "url.dll,FileProtocolHandler"}
	case "android":
		args = []string{"am", "start", "-a", "android.intent.action.VIEW", "-d"}
	default:
		return fmt.Errorf("unsupported platform: <%s>", runtime.GOOS)
	}

	args = append(args, url)
	logger.Debugf(ctx, "launching a browser using command '%s'", strings.Join(args, " "))
	return exec.Command(args[0], args[1:]...).Start()
}
