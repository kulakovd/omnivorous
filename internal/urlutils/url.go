package urlutils

import "net/url"

func Clone(orig *url.URL) *url.URL {
	newUrl := &url.URL{
		Scheme:      orig.Scheme,
		Opaque:      orig.Opaque,
		User:        orig.User,
		Host:        orig.Host,
		Path:        orig.Path,
		RawPath:     orig.RawPath,
		OmitHost:    orig.OmitHost,
		ForceQuery:  orig.ForceQuery,
		RawQuery:    orig.RawQuery,
		Fragment:    orig.Fragment,
		RawFragment: orig.RawFragment,
	}
	return newUrl
}
