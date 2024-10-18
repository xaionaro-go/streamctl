package expression

import (
	"io"
	"net/http"
)

var funcMap = map[string]interface{}{
	"devnull": func(args ...any) string {
		return ""
	},
	"httpGET": func(urlString string) string {
		resp, err := http.Get(urlString)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		return string(b)
	},
	"httpGETIgnoreErrors": func(urlString string) string {
		resp, err := http.Get(urlString)
		if err != nil {
			return ""
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return ""
		}

		return string(b)
	},
}
