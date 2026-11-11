package api

import "net/http"

// ---- inbound webhooks (raw route; HMAC-authenticated) ----

func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	// TODO(wave): verify X-Airval-Signature/X-Airval-Timestamp with the trigger's unsealed secret
	// (Keyring.Unseal(t.SecretEnc, t.SecretKeyVersion, t.ID[:])), map payload → variables/labels, create runs (milestone 8).
	s.writeError(w, r, notImplemented("webhook triggers"))
}
