//go:build ee

// Package branding implements org-level white-labeling: product name, logo and an accent ramp chosen
// from a curated set (each one is a full 50–900 ramp defined in web/src/styles/tokens.css, validated for
// contrast like the default iris ramp). Arbitrary colors are deliberately not accepted.
package branding

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/store"
)

const settingsKey = "branding"

// Accents is the curated accent set (tokens.css `[data-accent=...]`).
var Accents = []string{"iris", "indigo", "ocean", "teal", "plum", "rose"}

// MaxLogoBytes bounds the decoded logo.
const MaxLogoBytes = 128 << 10

// Branding is the stored and API shape.
type Branding struct {
	ProductName string  `json:"product_name"`
	Logo        *string `json:"logo"`
	Accent      string  `json:"accent"`
}

// Routes registers branding endpoints.
func Routes(r chi.Router, h httpx.H) {
	a := r.With(h.Host.Authenticate)
	a.Get("/v1/orgs/{org}/branding", h.Wrap("getBranding", func(w http.ResponseWriter, r *http.Request) error {
		orgID, _, err := h.RequireOrg(r, store.RoleViewer)
		if err != nil {
			return err
		}
		b := Branding{Accent: "iris"}
		raw, err := h.Host.Store().GetOrgSetting(r.Context(), orgID, settingsKey)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			_ = json.Unmarshal(raw, &b)
		}
		return httpx.JSON(w, http.StatusOK, b)
	}))
	a.Put("/v1/orgs/{org}/branding", h.Wrap("putBranding", func(w http.ResponseWriter, r *http.Request) error {
		orgID, p, err := h.RequireOrg(r, store.RoleAdmin)
		if err != nil {
			return err
		}
		if err := httpx.RequireUser(p); err != nil {
			return err
		}
		var in Branding
		if err := httpx.Decode(r, &in); err != nil {
			return err
		}
		if err := Validate(&in); err != nil {
			return httpx.Invalid("%v", err)
		}
		b, _ := json.Marshal(in)
		if err := h.Host.Store().PutOrgSetting(r.Context(), orgID, settingsKey, b); err != nil {
			return err
		}
		h.Audit(r, orgID, "branding.update", "org:"+orgID.String(),
			map[string]any{"product_name": in.ProductName, "accent": in.Accent, "logo": in.Logo != nil})
		return httpx.JSON(w, http.StatusOK, in)
	}))
}

var logoRe = regexp.MustCompile(`^data:image/(png|jpeg|webp|svg\+xml);base64,([A-Za-z0-9+/]+={0,2})$`)

var svgForbidden = regexp.MustCompile(`(?i)<script|<foreignobject|javascript:|\son[a-z]+\s*=|<iframe|<embed|<object|xlink:href\s*=\s*["']\s*(?:https?:|data:)|href\s*=\s*["']\s*(?:https?:|javascript:)`)

// Validate normalizes and checks b.
func Validate(b *Branding) error {
	b.ProductName = strings.TrimSpace(b.ProductName)
	if len([]rune(b.ProductName)) > 40 {
		return errors.New("product_name must be at most 40 characters")
	}
	if strings.IndexFunc(b.ProductName, unicode.IsControl) >= 0 {
		return errors.New("product_name must not contain control characters")
	}
	if b.Accent == "" {
		b.Accent = "iris"
	}
	if !slices.Contains(Accents, b.Accent) {
		return errors.New("accent must be one of " + strings.Join(Accents, ", "))
	}
	if b.Logo != nil && *b.Logo == "" {
		b.Logo = nil
	}
	if b.Logo != nil {
		m := logoRe.FindStringSubmatch(*b.Logo)
		if m == nil {
			return errors.New("logo must be a base64 data: URL of a PNG, JPEG, WebP or SVG image")
		}
		data, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			return errors.New("logo is not valid base64")
		}
		if len(data) > MaxLogoBytes {
			return errors.New("logo must be at most 128 KiB")
		}
		if !sniff(m[1], data) {
			return errors.New("logo content does not match its image type")
		}
		if m[1] == "svg+xml" && svgForbidden.Match(data) {
			return errors.New("SVG logos must not contain scripts, event handlers or external references")
		}
	}
	return nil
}

func sniff(kind string, data []byte) bool {
	switch kind {
	case "png":
		return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case "jpeg":
		return bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF})
	case "webp":
		return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	case "svg+xml":
		return bytes.Contains(bytes.ToLower(data[:min(len(data), 1024)]), []byte("<svg"))
	}
	return false
}
