package scrape

import (
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bcrusher29/solaris/config"

	"github.com/PuerkitoBio/goquery"
	"github.com/elazarl/goproxy"
	logging "github.com/op/go-logging"
	"github.com/robertkrimen/otto"
)

var (
	log = logging.MustGetLogger("proxy")

	// Proxy ...
	Proxy = goproxy.NewProxyHttpServer()

	// ProxyPort ...
	ProxyPort = 65222

	hostMatch = "^.*$"
)

// AlwaysHTTPMitm ...
var AlwaysHTTPMitm goproxy.FuncHttpsHandler = func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: CustomTLS(&goproxy.GoproxyCa)}, host
}

var goproxySignerVersion = ":goroxy1"

// CustomTLS ...
func CustomTLS(ca *tls.Certificate) func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) {
	return func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) {
		config := &tls.Config{
			PreferServerCipherSuites: true,
			Certificates:             []tls.Certificate{*ca},
			InsecureSkipVerify:       true,
		}

		return config, nil
	}
}

func handleRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	// Removing these headers to ensure cloudflare is not taking these headers into account.
	req.Header.Del("Connection")
	req.Header.Del("Accept-Encoding")

	// req.Header.Del("Cookie")
	// req.Header.Del("Origin")

	if config.Get().InternalProxyLogging {
		dumpRequest(req, ctx, true, true)
	} else {
		dumpRequest(req, ctx, false, true)
	}

	bodyBytes, _ := ioutil.ReadAll(req.Body)
	defer req.Body.Close()

	ctx.UserData = bodyBytes
	req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

	return req, nil
}

func handleResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	defer ctx.Req.Body.Close()

	if config.Get().InternalProxyLogging {
		dumpResponse(resp, ctx, true, false)
	} else {
		dumpResponse(resp, ctx, false, false)
	}

	if resp == nil {
		return resp
	}

	if resp.StatusCode == 503 && (resp.Header.Get("Server") == "cloudflare-nginx" || resp.Header.Get("Server") == "cloudflare") {
		if cfResponse, passed := solveCloudFlare(resp, ctx); passed {
			bodyBytes := ctx.UserData.([]byte)
			ctx.Req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

			if cfResponse != nil {
				for _, cookie := range cfResponse.Cookies() {
					ctx.Req.AddCookie(cookie)
				}
			}

			if respRetry, err := ctx.RoundTrip(ctx.Req); err == nil && respRetry != nil {
				if set := cfResponse.Header.Get("Set-Cookie"); set != "" {
					respRetry.Header.Add("Set-Cookie", set)
				}

				return respRetry
			}
		}
	}

	return resp
}

func dumpRequest(req *http.Request, ctx *goproxy.ProxyCtx, details bool, body bool) {
	log.Debugf("[%d] --> %s %s", ctx.Session, req.Method, req.URL)

	if !details {
		return
	}

	if req == nil {
		log.Debugf("REQUEST: nil")
		return
	}

	dump, _ := httputil.DumpRequest(req, body)
	log.Debugf("REQUEST:\n%s", dump)
}

func dumpResponse(resp *http.Response, ctx *goproxy.ProxyCtx, details bool, body bool) {
	if resp != nil {
		log.Debugf("[%d] <-- %d %s", ctx.Session, resp.StatusCode, ctx.Req.URL.String())
	} else {
		log.Debugf("[%d] <-- ERR %s", ctx.Session, ctx.Req.URL.String())
		return
	}

	if !details {
		return
	}

	if resp == nil {
		log.Debugf("RESPONSE: nil")
		return
	}

	dump, _ := httputil.DumpResponse(resp, body)
	log.Debugf("RESPONSE:\n%s", dump)
}

func solveCloudFlare(resp *http.Response, ctx *goproxy.ProxyCtx) (*http.Response, bool) {
	req := cloneRequest(ctx.Req)
	for _, cookie := range resp.Cookies() {
		req.AddCookie(cookie)
	}
	rurl := req.URL
	originalURL, _ := resp.Location()
	if originalURL == nil {
		originalURL = req.URL
	}

	if strings.Contains(rurl.String(), "chk_jschl") {
		// We are in deadloop
		return nil, false
	}

	time.Sleep(time.Duration(4) * time.Second)
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	buff := bytes.NewBuffer(body)
	dom, err := goquery.NewDocumentFromReader(buff)

	host := strings.Replace(rurl.Host, ":443", "", -1)

	js := dom.Find("script:contains(\"s,t,o,p,b,r,e,a,k,i,n,g\")").Text()
	if strings.Contains(js, "parseInt") {
		re1 := regexp.MustCompile("setTimeout\\(function\\(\\){\\s+(var s,t,o,p,b,r,e,a,k,i,n,g,f.+?\\r?\\n[\\s\\S]+?a\\.value =.+?)\\r?\\n")
		re2 := regexp.MustCompile("a\\.value = (parseInt\\(.+?\\)).+")
		re3 := regexp.MustCompile("\\s{3,}[a-z](?: = |\\.).+")
		re4 := regexp.MustCompile("[\\n\\\\']")

		js = re1.FindAllStringSubmatch(js, -1)[0][1]
		js = re2.ReplaceAllString(js, re2.FindAllStringSubmatch(js, -1)[0][1])
		js = re3.ReplaceAllString(js, "")
		js = re4.ReplaceAllString(js, "")
		js = strings.Replace(js, "return", "", -1)

		jsEngine := otto.New()
		data, err := jsEngine.Eval(js)
		if err != nil {
			return nil, false
		}
		checksum, err := data.ToInteger()
		if err != nil {
			return nil, false
		}
		checksum += int64(len(host))
		if err != nil {
			return nil, false
		}

		jschlVc, _ := dom.Find("input[name=\"jschl_vc\"]").Attr("value")
		pass, _ := dom.Find("input[name=\"pass\"]").Attr("value")
		jschlAnswer := strconv.Itoa(int(checksum))

		u := rurl.Scheme + "://" + rurl.Host + "/cdn-cgi/l/chk_jschl"
		ur, err := url.Parse(u)
		q := ur.Query()
		q.Add("jschl_vc", jschlVc)
		q.Add("pass", pass)
		q.Add("jschl_answer", jschlAnswer)
		ur.RawQuery = q.Encode()

		req.Header.Del("Referer")
		req.Header.Add("Referer", rurl.String())

		req.URL = ur
		if cfResponse, err := ctx.RoundTrip(req); cfResponse != nil && err == nil {
			return cfResponse, true
		}
	}

	re1 := regexp.MustCompile("setTimeout\\(function\\(\\){\\s+(var s,t,o,p,b,r,e,a,k,i,n,g,f.+?\\r?\\n[\\s\\S]+?a\\.value =.+?)\\r?\\n")
	re2 := regexp.MustCompile("\\s{3,}[a-z](?: = |\\.).+")
	re3 := regexp.MustCompile("[\\n\\\\']")
	re4 := regexp.MustCompile(";\\s*\\d+\\s*$")
	re5 := regexp.MustCompile("a\\.value\\s*\\=")

	js = re1.FindAllStringSubmatch(js, -1)[0][1]
	js = strings.Replace(js, "s,t,o,p,b,r,e,a,k,i,n,g,f,", "s,t = \""+host+"\",o,p,b,r,e,a,k,i,n,g,f,", 1)
	js = re2.ReplaceAllString(js, "")
	js = re3.ReplaceAllString(js, "")
	js = re4.ReplaceAllString(js, "")
	js = re5.ReplaceAllString(js, "return ")

	jsEngine := otto.New()
	data, err := jsEngine.Eval("(function () {" + js + "})()")
	if err != nil {
		return nil, false
	}

	checksum, err := data.ToInteger()
	if err != nil {
		return nil, false
	}

	checksum += int64(len(host))
	if err != nil {
		return nil, false
	}

	jschlVc, _ := dom.Find("input[name=\"jschl_vc\"]").Attr("value")
	pass, _ := dom.Find("input[name=\"pass\"]").Attr("value")

	u := rurl.Scheme + "://" + rurl.Host + "/cdn-cgi/l/chk_jschl"
	ur, err := url.Parse(u)
	q := ur.Query()
	q.Add("jschl_vc", jschlVc)
	q.Add("pass", pass)
	ur.RawQuery = q.Encode() + "&jschl_answer=" + data.String()

	req.Header.Del("Content-Type")
	req.Header.Del("Content-Length")
	req.Header.Del("Origin")

	if originalURL != nil {
		req.Header.Del("Referer")
		req.Header.Add("Referer", strings.Replace(originalURL.String(), ":443", "", -1))
	}

	req.URL = ur
	req.Method = "GET"
	req.ContentLength = 0

	if config.Get().InternalProxyLogging {
		dumpRequest(req, ctx, true, true)
	} else {
		dumpRequest(req, ctx, false, true)
	}
	cfResponse, err := ctx.RoundTrip(req)
	if config.Get().InternalProxyLogging {
		dumpResponse(cfResponse, ctx, true, true)
	} else {
		dumpResponse(cfResponse, ctx, false, true)
	}

	if cfResponse != nil && err == nil {
		if cfResponse.StatusCode == 503 {
			return nil, false
		}

		return cfResponse, true
	}

	log.Debugf("Could not finish CloudFlare: %#v", err)
	return nil, false
}

func cloneRequest(r *http.Request) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r
	// deep copy of the Header
	r2.Header = make(http.Header)
	for k, s := range r.Header {
		r2.Header[k] = s
	}
	return r2
}

// StartProxy starts HTTP/HTTPS proxy for debugging
func StartProxy() *http.Server {
	Proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile(hostMatch))).
		HandleConnect(AlwaysHTTPMitm)

	Proxy.OnRequest().DoFunc(handleRequest)
	Proxy.OnResponse().DoFunc(handleResponse)

	Proxy.Verbose = false
	Proxy.KeepDestinationHeaders = true

	if config.Get().ProxyURL != "" {
		proxyURL, _ := url.Parse(config.Get().ProxyURL)
		Proxy.Tr.Proxy = GetProxyURL(proxyURL)
		log.Debugf("Setting up proxy for internal proxy: %s", config.Get().ProxyURL)
	} else {
		Proxy.Tr.Proxy = GetProxyURL(nil)
	}

	if config.Get().InternalDNSEnabled {
		Proxy.Tr.Dial = CustomDial
	}

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(ProxyPort),
		Handler: Proxy,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Warningf("Could not start internal proxy: %s", err)
		}
	}()

	return srv
}
