package jwt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	_ "github.com/coreos/go-oidc/v3/oidc"
	"github.com/getkin/kin-openapi/openapi3filter"
	"golang.org/x/oauth2"
)

// need to design an interface/struct that takes in
// connection info to keycloak and then handles verification.
// verification is three steps:
// 1. verify token is valid ie not expired and signed by valid algo
// 2. check token claims if claims are not sufficient ie missing from the token,
//		or not all are present, reject the token.
// 3. put the claims and user info into the request cxt such that the user is authenticated

var (
	ErrNoAuthHeader      = errors.New("no auth header")
	ErrInvalidAuthHeader = errors.New("invalid auth header")
	ErrClaimsNotFound    = errors.New("claims not found")
	ErrClaimsInvalid     = errors.New("claims invalid")
)

const (
	ClaimsContextKey = "jwt_claims"
)

type KeycloakJWSValidator struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

func NewKeycloakJWSValidator(issuer string, clientID string) (*KeycloakJWSValidator, error) {
	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return nil, err
	}
	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	return &KeycloakJWSValidator{verifier: verifier, provider: provider}, nil
}

func (k *KeycloakJWSValidator) Validate(jws string) (interface{}, error) {
	idToken, err := k.verifier.Verify(context.Background(), jws)
	if err != nil {
		return nil, err
	}
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		log.Printf("Error parsing token claims: %v", err)
		return nil, err
	}
	return claims, nil
}

func GetJwsFromRequest(r *http.Request) (string, error) {
	authHdr := r.Header.Get("Authorization")
	if authHdr == "" {
		return "", ErrNoAuthHeader
	}
	prefix := "Bearer "
	if !strings.HasPrefix(authHdr, prefix) {
		return "", ErrInvalidAuthHeader
	}
	return strings.TrimPrefix(authHdr, prefix), nil
}

func NewAuthenticator(v KeycloakJWSValidator) openapi3filter.AuthenticationFunc {
	return func(ctx context.Context, input *openapi3filter.AuthenticationInput) error {
		return Authenticate(v, ctx, input)
	}
}

func (k *KeycloakJWSValidator) UserInfo(ctx context.Context, accessToken string) (map[string]interface{}, error) {
	userinfo, err := k.provider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: accessToken,
	}))
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := userinfo.Claims(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func CheckTokenClaims(claims map[string]interface{}, expectedScopes []string) error {
	scopesStr, ok := claims["scope"].(string)
	if !ok {
		return ErrClaimsInvalid
	}
	scopes := strings.Split(scopesStr, " ")
	scopeSet := make(map[string]struct{})
	for _, scope := range scopes {
		scopeSet[scope] = struct{}{}
	}
	for _, scope := range expectedScopes {
		if _, ok := scopeSet[scope]; !ok {
			return ErrClaimsNotFound
		}
	}
	return nil
}

func Authenticate(validator KeycloakJWSValidator, cxt context.Context, input *openapi3filter.AuthenticationInput) error {
	if input.SecuritySchemeName != "keycloakOAuth" {
		return fmt.Errorf("auth type should be of keycloakOAuth: but was %v", input.SecuritySchemeName)
	}
	jws, err := GetJwsFromRequest(input.RequestValidationInput.Request)
	if err != nil {
		return err
	}
	raw, err := validator.Validate(jws)
	if err != nil {
		return err
	}
	claim, ok := raw.(map[string]interface{})
	if !ok {
		return ErrClaimsInvalid
	}
	err = CheckTokenClaims(claim, input.Scopes)
	if err != nil {
		return err
	}
	cxt = context.WithValue(cxt, ClaimsContextKey, claim)
	input.RequestValidationInput.Request = input.RequestValidationInput.Request.WithContext(cxt)
	return nil
}
