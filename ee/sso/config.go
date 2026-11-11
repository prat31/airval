//go:build ee

// Package sso implements per-organization single sign-on: OIDC (authorization code + PKCE, state and
// nonce, ID token verification) and SAML 2.0 (SP-initiated, HTTP-POST ACS). Successful sign-ins create a
// regular core session (auth.Service.NewSession), so the rest of the app is unaware of how a user signed in.
package sso

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/ee/roles"
	"github.com/prat31/airval/internal/store"
)

// Config is the provider configuration stored in ee_sso_providers.config.
type Config struct {
	// OIDC
	Issuer   string   `json:"issuer,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
	// SAML
	IDPMetadataURL string `json:"idp_metadata_url,omitempty"`
	IDPMetadataXML string `json:"idp_metadata_xml,omitempty"`
	EmailAttribute string `json:"email_attribute,omitempty"`
	NameAttribute  string `json:"name_attribute,omitempty"`
	// Common
	AllowedDomains  []string      `json:"allowed_domains,omitempty"`
	RoleClaim       string        `json:"role_claim,omitempty"`
	RoleMapping     []MappingRule `json:"role_mapping,omitempty"`
	DefaultRole     store.Role    `json:"default_role,omitempty"`
	JITProvisioning bool          `json:"jit_provisioning"`
	SyncRoles       bool          `json:"sync_roles"`
}

// MappingRule maps a claim/attribute/group value to a role reference (built-in or custom:<id>).
type MappingRule struct {
	Value string `json:"value"`
	Role  string `json:"role"`
}

// ParseConfig decodes a stored config.
func ParseConfig(raw json.RawMessage) Config {
	var c Config
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

func (c Config) scopes() []string {
	s := []string{"openid", "email", "profile"}
	for _, x := range c.Scopes {
		if x = strings.TrimSpace(x); x != "" && !slices.Contains(s, x) {
			s = append(s, x)
		}
	}
	return s
}

func (c Config) defaultRole() store.Role {
	if c.DefaultRole.Valid() && c.DefaultRole != store.RoleOwner {
		return c.DefaultRole
	}
	return store.RoleViewer
}

// Normalize validates c for kind and org; custom roles referenced by the mapping must belong to orgID.
func (c *Config) Normalize(kind string, customRoles []store.CustomRole) error {
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	switch kind {
	case "oidc":
		u, err := url.Parse(c.Issuer)
		if c.Issuer == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("issuer must be an http(s) URL")
		}
		if c.ClientID == "" {
			return fmt.Errorf("client_id is required")
		}
		c.IDPMetadataURL, c.IDPMetadataXML, c.EmailAttribute, c.NameAttribute = "", "", "", ""
	case "saml":
		c.IDPMetadataURL = strings.TrimSpace(c.IDPMetadataURL)
		c.IDPMetadataXML = strings.TrimSpace(c.IDPMetadataXML)
		if (c.IDPMetadataURL == "") == (c.IDPMetadataXML == "") {
			return fmt.Errorf("set exactly one of idp_metadata_url or idp_metadata_xml")
		}
		if c.IDPMetadataURL != "" {
			if u, err := url.Parse(c.IDPMetadataURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("idp_metadata_url must be an http(s) URL")
			}
		}
		if len(c.IDPMetadataXML) > 512<<10 {
			return fmt.Errorf("idp_metadata_xml is too large")
		}
		c.Issuer, c.ClientID, c.Scopes = "", "", nil
	default:
		return fmt.Errorf("kind must be oidc or saml")
	}
	var doms []string
	for _, d := range c.AllowedDomains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d == "" {
			continue
		}
		if strings.ContainsAny(d, "@/ ") || !strings.Contains(d, ".") {
			return fmt.Errorf("invalid allowed domain %q", d)
		}
		if !slices.Contains(doms, d) {
			doms = append(doms, d)
		}
	}
	c.AllowedDomains = doms
	if c.DefaultRole == "" {
		c.DefaultRole = store.RoleViewer
	}
	if !c.DefaultRole.Valid() || c.DefaultRole == store.RoleOwner {
		return fmt.Errorf("default_role must be admin, editor or viewer")
	}
	c.RoleClaim = strings.TrimSpace(c.RoleClaim)
	for i := range c.RoleMapping {
		m := &c.RoleMapping[i]
		m.Value = strings.TrimSpace(m.Value)
		if m.Value == "" {
			return fmt.Errorf("role mapping values must not be empty")
		}
		if err := ValidateRoleRef(m.Role, customRoles); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRoleRef accepts admin/editor/viewer or a custom role of the org (never owner: ownership is
// only granted by another owner).
func ValidateRoleRef(ref string, customRoles []store.CustomRole) error {
	role, custom, err := roles.ParseRef(ref)
	if err != nil {
		return err
	}
	if custom != nil {
		if !slices.ContainsFunc(customRoles, func(c store.CustomRole) bool { return c.ID == *custom }) {
			return fmt.Errorf("custom role %s not found", custom)
		}
		return nil
	}
	if role == store.RoleOwner {
		return fmt.Errorf("the owner role cannot be granted by an identity provider")
	}
	return nil
}

// MapRole picks the role for a user's claim values: the highest matching built-in role, else the
// first matching custom role. ok=false when nothing matches.
func MapRole(rules []MappingRule, values []string) (store.Role, *uuid.UUID, bool) {
	var best store.Role
	var custom *uuid.UUID
	for _, rule := range rules {
		if !slices.ContainsFunc(values, func(v string) bool { return strings.EqualFold(strings.TrimSpace(v), rule.Value) }) {
			continue
		}
		role, cid, err := roles.ParseRef(rule.Role)
		if err != nil || role == store.RoleOwner {
			continue
		}
		if cid != nil {
			if custom == nil {
				custom = cid
			}
		} else if role.Rank() > best.Rank() {
			best = role
		}
	}
	if best != "" {
		return best, nil, true
	}
	if custom != nil {
		return "", custom, true
	}
	return "", nil, false
}

// DomainAllowed reports whether email's domain is allowed.
func (c Config) DomainAllowed(email string) bool {
	if len(c.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	return at > 0 && slices.Contains(c.AllowedDomains, strings.ToLower(email[at+1:]))
}

// providerJSON is the API view of a provider (secrets are never returned).
type providerJSON struct {
	ID              uuid.UUID `json:"id"`
	OrgID           uuid.UUID `json:"org_id"`
	Slug            string    `json:"slug"`
	Kind            string    `json:"kind"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Config          Config    `json:"config"`
	HasClientSecret bool      `json:"has_client_secret"`
	RedirectURI     string    `json:"redirect_uri"`
	MetadataURL     string    `json:"metadata_url,omitempty"`
	EntityID        string    `json:"entity_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
