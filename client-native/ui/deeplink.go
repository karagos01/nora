package ui

import (
	"net/url"
	"strings"
)

type DeepLink struct {
	Type   string // "contact", "invite", "group"
	Key    string // public key (for contact)
	Name   string // display name (query param)
	Server string // server host (for invite)
	Code   string // invite code (for invite)
}

// DeepLinkInvite holds a pending invite deep link awaiting user confirmation.
type DeepLinkInvite struct {
	Server string
	Code   string
}

// ParseDeepLink parses a nora:// URL.
// Formats:
//   nora://contact/{publicKey}?name=...
//   nora://invite/{host}/{code}
//   nora://group/{groupID}
func ParseDeepLink(link string) *DeepLink {
	if !strings.HasPrefix(link, "nora://") {
		return nil
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	host := u.Host // nora://contact/... → host="contact"

	// nora://contact/{key}?name=...
	if host == "contact" && len(parts) >= 1 && parts[0] != "" {
		return &DeepLink{
			Type: "contact",
			Key:  parts[0],
			Name: u.Query().Get("name"),
		}
	}

	// nora://invite/{host}/{code}
	if host == "invite" && len(parts) >= 2 {
		return &DeepLink{
			Type:   "invite",
			Server: parts[0],
			Code:   parts[1],
		}
	}

	// nora://group/{id}
	if host == "group" && len(parts) >= 1 {
		return &DeepLink{
			Type: "group",
			Key:  parts[0],
		}
	}

	return nil
}
