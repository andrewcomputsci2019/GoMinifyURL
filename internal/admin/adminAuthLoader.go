package admin

import (
	"GOMinifyURL/internal/api/admin"
	"GOMinifyURL/internal/middleware/auth"
	"GOMinifyURL/internal/servicenames"
	"net/http"
)

func getAuthHandler() (func(handler http.Handler) http.Handler, error) {
	spec, err := admin.GetSwagger()
	if err != nil {
		return nil, err
	}
	return auth.LoadAuthMiddleWare(spec, servicenames.AdminServiceName)
}
