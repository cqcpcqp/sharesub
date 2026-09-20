package httpapi

import "net/http"

func (s *Server) accountCodexState(w http.ResponseWriter, r *http.Request) {
	v, err := s.app.AccountCodexState(r.Context(), currentUser(r), r.PathValue("accountID"), r.Method == http.MethodPost)
	writeResult(w, v, err)
}
