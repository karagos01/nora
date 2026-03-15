package linkpreview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	maxBodySize  = 1 * 1024 * 1024 // 1MB
	fetchTimeout = 5 * time.Second
	userAgent    = "NORA-Bot/1.0 (+link-preview)"
)

// safeTransport checks the target IP on every TCP connection,
// so even after HTTP redirects it re-verifies we're not targeting a private network.
var safeTransport = &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address: %w", err)
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}
		var d net.Dialer
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if isPrivateIP(ip) {
				continue
			}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ipStr, port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("no public IP available for %s", host)
	},
}

var safeClient = &http.Client{
	Timeout:   fetchTimeout,
	Transport: safeTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

type Result struct {
	URL         string
	Title       string
	Description string
	ImageURL    string
	SiteName    string
}

// Fetch downloads OpenGraph metadata from the given URL.
// Returns nil if the URL is invalid, is private, or contains no metadata.
func Fetch(rawURL string) (*Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	// SSRF protection: safeClient checks IP on every TCP connection (including redirects)
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return nil, fmt.Errorf("not HTML: %s", ct)
	}

	body := io.LimitReader(resp.Body, maxBodySize)
	return parseOG(body, rawURL)
}

func parseOG(r io.Reader, rawURL string) (*Result, error) {
	tokenizer := html.NewTokenizer(r)
	res := &Result{URL: rawURL}
	var titleTag strings.Builder
	inTitle := false
	inHead := true

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			goto done
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tag := string(tn)

			if tag == "body" {
				// Stop parsing after end of <head>
				goto done
			}
			if tag == "/head" {
				goto done
			}

			if tag == "title" && inHead {
				inTitle = true
				continue
			}

			if tag == "meta" && hasAttr {
				attrs := readAttrs(tokenizer)
				prop := attrs["property"]
				if prop == "" {
					prop = attrs["name"]
				}
				content := attrs["content"]
				if content == "" {
					continue
				}

				switch prop {
				case "og:title":
					res.Title = content
				case "og:description":
					res.Description = content
				case "og:image":
					res.ImageURL = content
				case "og:site_name":
					res.SiteName = content
				case "description":
					if res.Description == "" {
						res.Description = content
					}
				}
			}
		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			tag := string(tn)
			if tag == "title" {
				inTitle = false
			}
			if tag == "head" {
				inHead = false
				goto done
			}
		case html.TextToken:
			if inTitle {
				titleTag.Write(tokenizer.Text())
			}
		}
	}

done:
	// Fallback to <title> tag if og:title is missing
	if res.Title == "" {
		res.Title = strings.TrimSpace(titleTag.String())
	}

	// Nothing found
	if res.Title == "" && res.Description == "" {
		return nil, nil
	}

	// Truncate description
	if len(res.Description) > 300 {
		res.Description = res.Description[:300] + "…"
	}

	return res, nil
}

func readAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, val, more := z.TagAttr()
		if len(key) > 0 {
			attrs[string(key)] = string(val)
		}
		if !more {
			break
		}
	}
	return attrs
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []struct {
		network *net.IPNet
	}{
		{mustParseCIDR("10.0.0.0/8")},
		{mustParseCIDR("172.16.0.0/12")},
		{mustParseCIDR("192.168.0.0/16")},
		{mustParseCIDR("127.0.0.0/8")},
		{mustParseCIDR("169.254.0.0/16")},
		{mustParseCIDR("::1/128")},
		{mustParseCIDR("fc00::/7")},
		{mustParseCIDR("fe80::/10")},
	}
	for _, r := range privateRanges {
		if r.network.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}
