package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/middleware/auth"
	"GOMinifyURL/internal/servicenames"
	"net/http"
)

// getAuthHandler Register the auth middleware. The rules parameter allows the caller to register validation handling
// for request body error handling etc
func getAuthHandler(rules ...auth.ErrorRule) (func(handler http.Handler) http.Handler, error) {
	spec, err := admin.GetSwagger()
	if err != nil {
		return nil, err
	}
	return auth.LoadAuthMiddleWare(spec, servicenames.AdminServiceName, rules...)
}
