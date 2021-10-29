package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"time"

	"proxy/proxy"

	"github.com/gorilla/mux"
)

type contextKey int
const LogBypassedKey contextKey = 0
var reqStorage map[string]http.Request

func main() {
	reqStorage = make(map[string]http.Request)
	caCert, caKey, err := proxy.LoadOrCreateCA("./key", "./cert")
	if err != nil {
		log.Fatal("could not create/load CA key pair: %w", err)
	}

	p, err := proxy.NewProxy(caCert, caKey)
	if err != nil {
		log.Fatal("could not create proxy: %w", err)
	}
	p.UseRequestModifier(RequestModifier)
	p.UseResponseModifier(ResponseModifier)

	router := mux.NewRouter().SkipClean(true)
	router.PathPrefix("").Handler(p)

	s := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}, // Disable HTTP/2
	}

	fmt.Println("Running server on: ", ":8080")

	err = s.ListenAndServe()
	if err != nil && errors.Is(err, http.ErrServerClosed) {
		log.Fatal("http server closed unexpected: %w", err)
	}
}

func RequestModifier(next proxy.RequestModifyFunc) proxy.RequestModifyFunc {
	return func(req *http.Request) {
		// now := time.Now()

		next(req)

		clone := req.Clone(req.Context())

		var body []byte

		if req.Body != nil {
			// TODO: Use io.LimitReader.
			var err error

			body, err = ioutil.ReadAll(req.Body)
			if err != nil {
				log.Printf("[ERROR] Could not read request body for logging: %v", err)
				return
			}

			req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
		}
		rID := RandomString(24)
		reqStorage[rID] = *clone

		ctx := context.WithValue(req.Context(), proxy.ReqIDKey, rID)
		*req = *req.WithContext(ctx)
	}
}


func ResponseModifier(next proxy.ResponseModifyFunc) proxy.ResponseModifyFunc {
	return func(res *http.Response) error {
		// now := time.Now()

		if err := next(res); err != nil {
			return err
		}

		if bypassed, _ := res.Request.Context().Value(LogBypassedKey).(bool); bypassed {
			return nil
		}

		reqID, _ := res.Request.Context().Value(proxy.ReqIDKey).(string)
		if reqID == "" {
			return errors.New("reqlog: request is missing ID")
		}

		clone := *res

		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return fmt.Errorf("reqlog: could not read response body: %w", err)
		}

		res.Body = ioutil.NopCloser(bytes.NewBuffer(body))

		reqClone := reqStorage[reqID]
		if reqClone.Body != nil {
			reqbody, err := ioutil.ReadAll(reqClone.Body)
			if err != nil {
				return fmt.Errorf("reqlog: could not read response body: %w", err)
			}
			reqClone.Body = ioutil.NopCloser(bytes.NewBuffer(reqbody))
		}
		fmt.Println(reqClone.URL, clone.Status)

		return nil
	}
}

//RandomString returns a random string of length of parameter n
func RandomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	var chars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}
