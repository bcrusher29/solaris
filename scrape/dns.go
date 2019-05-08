package scrape

import (
	"context"
	"crypto/tls"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bogdanovich/dns_resolver"
)

var (
	// opennicZones contains all zones from Opennic services.
	// List can be taken here: https://wiki.opennic.org/opennic/dot
	opennicZones = []string{
		"bbs",
		"chan",
		"cyb",
		"dyn",
		"geek",
		"gopher",
		"indy",
		"libre",
		"neo",
		"null",
		"o",
		"oss",
		"oz",
		"parody",
		"pirate",
		"free",
		"bazar",
		"coin",
		"emc",
		"lib",
		"fur",
		"bit",
		"ku",
		"te",
		"ti",
		"uu",
	}

	cloudflareResolver = CloudflareResolver()
	googleResolver     = GoogleResolver()
	opennicResolver    = OpennicResolver()

	resolverOpennic = dns_resolver.New([]string{"193.183.98.66", "172.104.136.243", "89.18.27.167"})

	dnsCacheResults sync.Map
	dnsCacheLocks   sync.Map
)

// CloudflareResolver returns Resolver that uses Cloudflare service on 1.1.1.1 and
// 1.0.0.1 on port 853.
//
// See https://developers.cloudflare.com/1.1.1.1/dns-over-tls/ for details.
func CloudflareResolver() *net.Resolver {
	return newResolver("cloudflare-dns.com", "1.1.1.1:853", "1.0.0.1:853")
}

// Quad9Resolver returns Resolver that uses Quad9 service on 9.9.9.9 and 149.112.112.112
// on port 853.
//
// See https://quad9.net/faq/ for details.
func Quad9Resolver() *net.Resolver {
	return newResolver("dns.quad9.net", "9.9.9.9:853", "149.112.112.112:853")
}

// GoogleResolver returns Resolver that uses Google Public DNS service on 8.8.8.8 and
// 8.8.4.4 on port 853.
//
// See https://developers.google.com/speed/public-dns/ for details.
func GoogleResolver() *net.Resolver {
	return newResolver("dns.google", "8.8.8.8:853", "8.8.4.4:853")
}

// OpennicResolver returns Resolver that uses Opennic Public DNS service on port 853.
//
// See https://servers.opennicproject.org/ for details.
func OpennicResolver() *net.Resolver {
	return newResolver("dot-de.blahdns.com", "159.69.198.101:853")
}

func newResolver(serverName string, addrs ...string) *net.Resolver {
	if serverName == "" {
		panic("dot: server name cannot be empty")
	}
	if len(addrs) == 0 {
		panic("dot: addrs cannot be empty")
	}
	var d net.Dialer
	cfg := &tls.Config{
		ServerName:         serverName,
		ClientSessionCache: tls.NewLRUClientSessionCache(0),
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := d.DialContext(ctx, "tcp", addrs[rand.Intn(len(addrs))])
			if err != nil {
				return nil, err
			}
			conn.(*net.TCPConn).SetKeepAlive(true)
			conn.(*net.TCPConn).SetKeepAlivePeriod(5 * time.Minute)
			return tls.Client(conn, cfg), nil
		},
	}
}

func resolve(addr string) ([]string, error) {
	// now := time.Now()
	// defer log.Debugf("Resolved %s in %s", addr, time.Since(now))

	if isOpennicDomain(getZone(addr)) {
		if ips, err := opennicResolver.LookupHost(context.TODO(), addr); err == nil {
			return ips, err
		}
		if ip := resolveAddr(addr); ip != "" {
			return []string{ip}, nil
		}
	}

	if ips, err := cloudflareResolver.LookupHost(context.TODO(), addr); err == nil {
		return ips, err
	}

	return googleResolver.LookupHost(context.TODO(), addr)
}

func getZone(addr string) string {
	ary := strings.Split(addr, ".")
	return ary[len(ary)-1]
}

func isOpennicDomain(zone string) bool {
	for _, z := range opennicZones {
		if z == zone {
			return true
		}
	}

	return false
}

// This is very dump solution.
// We have a sync.Map with results for resolving IPs
// and a sync.Map with mutexes for each map.
// Mutexes are needed because torrent files are resolved concurrently and so
// DNS queries run concurrently as well, thus DNS hosts can ban for
// doing so many queries. So we wait until first one is finished.
// Possibly need to cleanup saved IPs after some time.
// Each request is going through this workflow:
// Check saved -> Query Google/Quad9 -> Check saved -> Query Opennic -> Save
func resolveAddr(host string) (ip string) {
	if cached, ok := dnsCacheResults.Load(host); ok {
		return cached.(string)
	}

	var mu *sync.Mutex
	if m, ok := dnsCacheLocks.Load(host); ok {
		mu = m.(*sync.Mutex)
	} else {
		mu = &sync.Mutex{}
		dnsCacheLocks.Store(host, mu)
	}

	mu.Lock()

	defer func() {
		mu.Unlock()
		if strings.HasPrefix(ip, "127.") {
			return
		}

		dnsCacheResults.Store(host, ip)
	}()

	if cached, ok := dnsCacheResults.Load(host); ok {
		return cached.(string)
	}

	ips, err := resolverOpennic.LookupHost(host)
	if err == nil && len(ips) > 0 {
		ip = ips[0].String()
		return
	}

	return
}
