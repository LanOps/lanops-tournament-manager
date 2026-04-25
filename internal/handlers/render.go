package handlers

import (
	"log"
	"net/http"
	"strconv"
	"text/template"
	"time"

	"github.com/gorilla/csrf"
	"github.com/th0rn0/thournament/internal/auth"
)

// AssetVersion is set once at process start and appended as ?v=... to static
// asset URLs so browsers pick up new JS/CSS on every server restart without a
// hard refresh.
var AssetVersion = strconv.FormatInt(time.Now().Unix(), 10)

// render looks up the named template in the map and executes it, automatically
// injecting CSRF token, UserID, and IsAdmin from the request context.
func render(w http.ResponseWriter, r *http.Request, tmpls map[string]*template.Template, name string, data map[string]interface{}) {
	tmpl, ok := tmpls[name]
	if !ok {
		log.Printf("template %q not found", name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if data == nil {
		data = map[string]interface{}{}
	}

	if _, ok := data["CSRFToken"]; !ok {
		data["CSRFToken"] = csrf.Token(r)
	}
	if _, ok := data["UserID"]; !ok {
		if uid, ok2 := auth.UserIDFromContext(r.Context()); ok2 {
			data["UserID"] = uid
		}
	}
	if _, ok := data["IsAdmin"]; !ok {
		data["IsAdmin"] = auth.IsAdminFromContext(r.Context())
	}
	if _, ok := data["AssetVersion"]; !ok {
		data["AssetVersion"] = AssetVersion
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %q error: %v", name, err)
	}
}
