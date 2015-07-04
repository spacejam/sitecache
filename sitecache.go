package main

import (
	"bytes"
  "flag"
  "strings"
  "strconv"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/elazarl/goproxy"
	log "github.com/golang/glog"
	"github.com/golang/groupcache"
)

type NullReadCloser struct {
	*bytes.Buffer
}

func (n *NullReadCloser) Close() error { return nil }

type ProxyResponse struct {
	response *http.Response
}

func NewProxyResponse() *ProxyResponse {
	return &ProxyResponse{
		response: &http.Response{},
	}
}

func (w ProxyResponse) Header() http.Header {
	return http.Header{}
}

func (w ProxyResponse) Write(body []byte) (int, error) {
	b := &NullReadCloser{bytes.NewBufferString("")}
	b.Write(body)
	w.response.Body = b
	return len(body), nil
}

func (w ProxyResponse) WriteHeader(header int) {}

func main() {
  rpcPort := flag.Int("rpcPort", 9090, "port for groupcache ipc")
  proxyPort := flag.Int("proxyPort", 8080, "port for http proxy")
  peerPort := flag.Int("peerPort", 7070, "port for receiving peer updates")

  flag.Parse()

	me := "http://localhost:" + strconv.Itoa(*rpcPort)
	poolOpts := &groupcache.HTTPPoolOptions{
		BasePath: "/stuff",
	}

	cache := groupcache.NewHTTPPoolOpts(me, poolOpts)

	groupcache.NewGroup("", 64<<20, groupcache.GetterFunc(
		func(ctx groupcache.Context, key string, dest groupcache.Sink) error {
			log.V(2).Info("trying to get ", key)
			client := &http.Client{
				Timeout: time.Second * 5,
			}
			res, err := client.Get(key)
			if err != nil {
				return err
			}

			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return errors.New("server returned: " + string(res.Status))
			}

			data, err := ioutil.ReadAll(res.Body)
			if err != nil {
				return err
			}
			dest.SetBytes(data)
			return nil
		},
	))

	cache.Set("http://localhost:9090", "http://localhost:9091", "http://localhost:9092")

	proxy := goproxy.NewProxyHttpServer()
	//proxy.Verbose = true
	proxy.OnRequest().DoFunc(
		func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		log.V(2).Infof("Proxy got request for: %+v", *r.URL)
			r.URL = &url.URL{
				Path: "/stuff/" + r.URL.Scheme + "://" + r.URL.Host + r.URL.Path,
			}
			pr := NewProxyResponse()
			cache.ServeHTTP(pr, r)
			log.V(3).Infof("body: %v", pr.response.Body)
			data, err := ioutil.ReadAll(pr.response.Body)
			if err != nil {
				log.V(1).Infof("Failed to read response %v", err)
			}
			return r, goproxy.NewResponse(r, goproxy.ContentTypeText, http.StatusForbidden, string(data))
		},
	)

	mux := http.NewServeMux()
  mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
    data, err := ioutil.ReadAll(r.Body)
    if err != nil {
      log.V(1).Infof("Failed to read response %v", err)
    }
    cache.Set(strings.Split(string(data), ",")...)
  })

  log.Infoln("Starting rpc instance.")
	go func() { log.Fatal(http.ListenAndServe(":" + strconv.Itoa(*rpcPort), cache)) }()

  log.Infoln("Starting peer update listener.")
  go func() { log.Fatal(http.ListenAndServe(":" + strconv.Itoa(*peerPort), mux)) }()
  log.Infoln("Starting proxy instance.")
	log.Fatal(http.ListenAndServe(":" + strconv.Itoa(*proxyPort), proxy))
}
