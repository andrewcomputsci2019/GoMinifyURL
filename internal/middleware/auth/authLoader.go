package auth

import (
	"GOMinifyURL/internal/middleware/jwt"
	"context"
	"errors"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	middleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/spf13/viper"
)

type ErrorMatcher func(error) bool

type ErrorHandler func(http.ResponseWriter, *http.Request, error)

type ErrorRule struct {
	Matcher ErrorMatcher
	Handler ErrorHandler
}

func LoadAuthMiddleWare(swagger *openapi3.T, clientID string, rules ...ErrorRule) (func(next http.Handler) http.Handler, error) {
	if swagger == nil {
		return nil, errors.New("swagger is nil")
	}
	issuer := viper.GetString("OIDC_ISSUER_URL")
	excludeBodyFromSchemaMatching := viper.GetBool("VALIDATION_EXCLUDE_BODY_FROM_SCHEMA_CHECK")
	verifier, err := jwt.NewKeycloakJWSValidator(issuer, clientID)
	if err != nil {
		return nil, err
	}
	ruleList := append([]ErrorRule(nil), rules...)
	validator := middleware.OapiRequestValidatorWithOptions(swagger, &middleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: jwt.NewAuthenticator(*verifier),
			ExcludeRequestBody: excludeBodyFromSchemaMatching,
		},
		DoNotValidateServers: true,
		ErrorHandlerWithOpts: func(ctx context.Context, err error, w http.ResponseWriter, r *http.Request, opts middleware.ErrorHandlerOpts) {
			// my security http error coder rewrite if need be
			if opts.StatusCode == http.StatusUnauthorized {
				var e *openapi3filter.SecurityRequirementsError
				switch {
				case errors.As(err, &e):
					for _, ers := range e.Errors {
						switch {
						case errors.Is(ers, jwt.ErrNoAuthHeader):
							http.Error(w, "missing auth header", http.StatusUnauthorized)
							return
						case errors.Is(ers, jwt.ErrInvalidAuthHeader):
							http.Error(w, "invalid auth header", http.StatusUnauthorized)
							return
						case errors.Is(ers, jwt.ErrClaimsNotFound):
							http.Error(w, "token malformed", http.StatusUnauthorized)
							return
						case errors.Is(ers, jwt.ErrClaimsInvalid):
							http.Error(w, "invalid permission to access route", http.StatusForbidden)
							return
						}
					}
					http.Error(w, "Security Check Failed", http.StatusInternalServerError)
					return
				}
			}
			for _, rule := range ruleList {
				if rule.Matcher(err) {
					rule.Handler(w, r, err)
					return
				}
			}
			http.Error(w, err.Error(), opts.StatusCode)
		},
	})
	return validator, nil
}
