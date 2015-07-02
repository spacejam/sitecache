package main

import (
  "time"
  "errors"
  "net/http"
  "io/ioutil"
  "net/url"
  "bytes"

  log "github.com/golang/glog"
	"github.com/elazarl/goproxy"
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
  return &ProxyResponse {
    response: &http.Response{},
  }
}

func (w ProxyResponse) Header() http.Header {
	return http.Header{}
}

func (w ProxyResponse) Write(body []byte) (int, error) {
	b := &NullReadCloser{ bytes.NewBufferString("") }
	b.Write(body)
	w.response.Body = b
	return len(body), nil
}

func (w ProxyResponse) WriteHeader(header int) {}

func main() {
  me := "http://10.0.0.1:9090"
  poolOpts := &groupcache.HTTPPoolOptions{
    BasePath: "/stuff",
  }
  peers := groupcache.NewHTTPPoolOpts(me, poolOpts)

  peers.Set(me)

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

	proxy := goproxy.NewProxyHttpServer()
	//proxy.Verbose = true
	proxy.OnRequest().DoFunc(
		func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
      log.V(2).Info("Proxy got request for: %+v \n" , *r.URL)
      r.URL = &url.URL{
        Path: "/stuff/"+r.URL.Scheme+"://"+r.URL.Host+r.URL.Path,
      }
			pr := NewProxyResponse()
        peers.ServeHTTP(pr, r)
      log.V(2).Infof("body: %v", pr.response.Body)
      data, err := ioutil.ReadAll(pr.response.Body)
      if err != nil {
          log.V(1).Infof("Failed to read response %v", err)
      }
			return r, goproxy.NewResponse(r, goproxy.ContentTypeText, http.StatusForbidden, string(data))
		},
	)

	log.Fatal(http.ListenAndServe(":8080", proxy))
}
