package reader

import (
	"net/url"
	"strings"
)

// CleanArticleURL removes known tracking fields without re-encoding or sorting
// functional query values. Signed URLs are kept intact. Feed and media addresses
// do not pass through this function: their query strings may grant access.
func CleanArticleURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}
	fields := strings.Split(u.RawQuery, "&")
	for _, field := range fields {
		name, err := url.QueryUnescape(strings.SplitN(field, "=", 2)[0])
		if err != nil {
			return raw
		}
		switch strings.ToLower(name) {
		case "signature", "sig", "x-amz-signature", "x-goog-signature", "policy", "key-pair-id":
			return raw
		}
	}
	kept := make([]string, 0, len(fields))
	changed := false
	for _, field := range fields {
		name, _ := url.QueryUnescape(strings.SplitN(field, "=", 2)[0])
		name = strings.ToLower(name)
		tracking := strings.HasPrefix(name, "utm_")
		switch name {
		case "fbclid", "gclid", "dclid", "msclkid", "mc_cid", "mc_eid", "igshid", "_hsenc", "_hsmi", "mkt_tok":
			tracking = true
		}
		if tracking {
			changed = true
		} else {
			kept = append(kept, field)
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = strings.Join(kept, "&")
	u.ForceQuery = false
	return u.String()
}

func articleLink(raw, base string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return ""
	}
	u = b.ResolveReference(u)
	fragment := u.Fragment
	address, err := WebURL(u.String())
	if err != nil {
		return ""
	}
	u, _ = url.Parse(CleanArticleURL(address))
	u.Fragment = fragment
	return u.String()
}
